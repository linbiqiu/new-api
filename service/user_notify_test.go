package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestNotifyUserFallsBackToEmailOnlyForDefaultFeishuUnavailable(t *testing.T) {
	emailCalls := 0
	err := notifyUserWithSenders(42, "user@example.com", dto.UserSetting{}, dto.Notify{}, notifySenderSet{
		feishu: func(int, dto.Notify) error { return ErrFeishuRecipientUnavailable },
		email: func(to string, _ dto.Notify) error {
			require.Equal(t, "user@example.com", to)
			emailCalls++
			return nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, emailCalls)
}

func TestNotifyUserDoesNotFallbackAfterFeishuSendFailure(t *testing.T) {
	sendErr := errors.New("feishu api timeout")
	emailCalls := 0
	err := notifyUserWithSenders(42, "user@example.com", dto.UserSetting{}, dto.Notify{}, notifySenderSet{
		feishu: func(int, dto.Notify) error { return sendErr },
		email: func(string, dto.Notify) error {
			emailCalls++
			return nil
		},
	})
	require.ErrorIs(t, err, sendErr)
	require.Zero(t, emailCalls)
}

func TestNotifyUserExplicitFeishuNeverFallsBack(t *testing.T) {
	emailCalls := 0
	err := notifyUserWithSenders(42, "user@example.com", dto.UserSetting{NotifyType: dto.NotifyTypeFeishuApp}, dto.Notify{}, notifySenderSet{
		feishu: func(int, dto.Notify) error { return ErrFeishuRecipientUnavailable },
		email: func(string, dto.Notify) error {
			emailCalls++
			return nil
		},
	})
	require.ErrorIs(t, err, ErrFeishuRecipientUnavailable)
	require.Zero(t, emailCalls)
}
