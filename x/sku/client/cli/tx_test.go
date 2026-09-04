package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsgUpdateProviderClearAPIURL(t *testing.T) {
	cmd := MsgUpdateProvider()
	flag := cmd.Flags().Lookup("clear-api-url")
	require.NotNil(t, flag)
	require.Equal(t, "false", flag.DefValue)

	require.NoError(t, cmd.Flags().Set("clear-api-url", "true"))
	clearAPIURL, err := cmd.Flags().GetBool("clear-api-url")
	require.NoError(t, err)
	require.True(t, clearAPIURL)
}
