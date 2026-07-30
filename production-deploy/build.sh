#!/usr/bin/env bash
set -euo pipefail

BLUE='\033[36m'
GREEN='\033[32m'
YELLOW='\033[33m'
RED='\033[31m'
RESET='\033[0m'
BOLD='\033[1m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUILD_ENV_FILE="${SCRIPT_DIR}/.build.env"

if [ -f "$BUILD_ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    source "$BUILD_ENV_FILE"
    set +a
fi

ACR_REGISTRY="${ACR_REGISTRY:-crpi-bij57v7e3thiuuod.cn-shenzhen.personal.cr.aliyuncs.com}"
ACR_NAMESPACE="${ACR_NAMESPACE:-ccpg_einwin}"
ACR_USERNAME="${ACR_USERNAME:-beacherlin}"

info()    { echo -e "${BLUE}[INFO]${RESET} $1"; }
success() { echo -e "${GREEN}[OK]${RESET} $1"; }
warn()    { echo -e "${YELLOW}[WARN]${RESET} $1"; }
error()   { echo -e "${RED}[ERROR]${RESET} $1" >&2; exit 1; }

usage() {
    cat <<EOF

Usage:
  $0 [--skip-push]

Options:
  --skip-push  Build and smoke-test locally without pushing to ACR
  -h, --help   Show this help

The image version is always read from ${REPO_ROOT}/VERSION.
The script only releases a clean commit that is already present on origin.
EOF
}

SKIP_PUSH=false
while [ $# -gt 0 ]; do
    case "$1" in
        --skip-push) SKIP_PUSH=true ;;
        -h|--help) usage; exit 0 ;;
        *) error "Unknown option: $1" ;;
    esac
    shift
done

command -v git >/dev/null || error "git is required"
command -v bun >/dev/null || error "bun is required"
command -v go >/dev/null || error "go is required"
command -v docker >/dev/null || error "docker is required"
command -v curl >/dev/null || error "curl is required"

cd "$REPO_ROOT"
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || error "Not a Git worktree: $REPO_ROOT"

DIRTY_FILES="$(git status --porcelain --untracked-files=all)"
if [ -n "$DIRTY_FILES" ]; then
    printf '%s\n' "$DIRTY_FILES" >&2
    error "Refusing to release a dirty worktree. Use a clean committed worktree."
fi

VERSION="$(tr -d '[:space:]' < "${REPO_ROOT}/VERSION")"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || \
    error "Invalid VERSION value: $VERSION"

SOURCE_COMMIT="$(git rev-parse HEAD)"
REMOTE_HEADS="$(git ls-remote --heads origin)" || error "Unable to query origin"
if ! awk -v commit="$SOURCE_COMMIT" '$1 == commit { found = 1 } END { exit !found }' <<< "$REMOTE_HEADS"; then
    error "Commit $SOURCE_COMMIT is not present on an origin branch. Commit and push it first."
fi

IMAGE_TAG="${ACR_REGISTRY}/${ACR_NAMESPACE}/new-api:${VERSION}"
BINARY_PATH="${REPO_ROOT}/new-api"
SMOKE_CONTAINER=""

cleanup() {
    if [ -n "$SMOKE_CONTAINER" ]; then
        docker rm -f "$SMOKE_CONTAINER" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

info "Version: $VERSION"
info "Commit:  $SOURCE_COMMIT"
info "Image:   $IMAGE_TAG"
info "Source:  $REPO_ROOT"

if [ "$SKIP_PUSH" = false ]; then
    info "Logging in to ACR..."
    if [ -n "${ACR_PASSWORD:-}" ]; then
        printf '%s' "$ACR_PASSWORD" | docker login --username="$ACR_USERNAME" --password-stdin "$ACR_REGISTRY" \
            || error "ACR login failed"
    else
        warn "ACR_PASSWORD is not set; using interactive login or existing credentials"
        docker login --username="$ACR_USERNAME" "$ACR_REGISTRY" || error "ACR login failed"
    fi

    TAG_CHECK_ERROR="$(mktemp)"
    if docker manifest inspect "$IMAGE_TAG" >/dev/null 2>"$TAG_CHECK_ERROR"; then
        rm -f "$TAG_CHECK_ERROR"
        error "ACR tag already exists and will not be overwritten: $IMAGE_TAG"
    elif ! grep -q "no such manifest" "$TAG_CHECK_ERROR"; then
        cat "$TAG_CHECK_ERROR" >&2
        rm -f "$TAG_CHECK_ERROR"
        error "Unable to prove that the ACR tag is unused"
    fi
    rm -f "$TAG_CHECK_ERROR"
fi

echo -e "\n${BOLD}${BLUE}Step 1/4: Build frontend and backend${RESET}"
cd "${REPO_ROOT}/web"
bun install --frozen-lockfile
bun run build:check

cd "$REPO_ROOT"
GENERATED_CHANGES="$(git status --porcelain --untracked-files=all)"
if [ -n "$GENERATED_CHANGES" ]; then
    printf '%s\n' "$GENERATED_CHANGES" >&2
    error "Frontend build changed the Git worktree; review and commit generated files before releasing"
fi

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOEXPERIMENT=greenteagc \
    go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=${VERSION}'" \
    -o "$BINARY_PATH" .
success "Frontend and linux/amd64 backend built"

echo -e "\n${BOLD}${BLUE}Step 2/4: Build Docker image${RESET}"
docker buildx build \
    --platform linux/amd64 \
    --provenance=false \
    --sbom=false \
    -t "$IMAGE_TAG" \
    -f deploy/Dockerfile.local \
    --load \
    .
IMAGE_SIZE="$(docker images "$IMAGE_TAG" --format '{{.Size}}')"
success "Image built: $IMAGE_TAG ($IMAGE_SIZE)"

echo -e "\n${BOLD}${BLUE}Step 3/4: Smoke test${RESET}"
SMOKE_CONTAINER="$(docker run -d --rm -p 127.0.0.1::3000 "$IMAGE_TAG")"
SMOKE_PORT="$(docker port "$SMOKE_CONTAINER" 3000/tcp | head -n 1)"
SMOKE_PORT="${SMOKE_PORT##*:}"
SMOKE_RESPONSE="$(mktemp)"
SMOKE_OK=false
for _ in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${SMOKE_PORT}/api/status" > "$SMOKE_RESPONSE"; then
        SMOKE_OK=true
        break
    fi
    sleep 2
done
if [ "$SMOKE_OK" = false ]; then
    docker logs "$SMOKE_CONTAINER" >&2 || true
    rm -f "$SMOKE_RESPONSE"
    error "Smoke test failed: /api/status did not become healthy"
fi
if ! grep -Eq "\"version\"[[:space:]]*:[[:space:]]*\"${VERSION}\"" "$SMOKE_RESPONSE"; then
    cat "$SMOKE_RESPONSE" >&2
    rm -f "$SMOKE_RESPONSE"
    error "Smoke test returned a version different from VERSION=$VERSION"
fi
rm -f "$SMOKE_RESPONSE"
docker rm -f "$SMOKE_CONTAINER" >/dev/null
SMOKE_CONTAINER=""
success "/api/status is healthy and reports version $VERSION"

echo -e "\n${BOLD}${BLUE}Step 4/4: Push image${RESET}"
if [ "$SKIP_PUSH" = false ]; then
    PUSH_OUTPUT="$(mktemp)"
    docker push "$IMAGE_TAG" | tee "$PUSH_OUTPUT"
    DIGEST="$(awk '/digest: sha256:/ { for (i = 1; i <= NF; i++) if ($i ~ /^sha256:/) digest = $i } END { print digest }' "$PUSH_OUTPUT")"
    rm -f "$PUSH_OUTPUT"
    [ -n "$DIGEST" ] || error "Image push completed but no digest was reported"
    success "Image pushed: $IMAGE_TAG"
    success "Digest: $DIGEST"
else
    warn "ACR push skipped; no production deployment should use this local-only image"
fi

echo -e "\n${BOLD}${GREEN}Build complete${RESET}"
echo "Version: $VERSION"
echo "Commit:  $SOURCE_COMMIT"
echo "Image:   $IMAGE_TAG"
echo "Size:    $IMAGE_SIZE"
echo ""
echo "Pushing an image does not deploy production. Follow LOCAL-BUILD-ACR-PROD-DEPLOY.md"
