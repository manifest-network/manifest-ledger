package cli

import (
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cometbft/cometbft/crypto/tmhash"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// GetWithdrawResultCmd decodes a committed transaction's withdrawal response.
func GetWithdrawResultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw-result [tx-hash]",
		Short: "Decode a successful, committed withdrawal transaction response",
		Long: `Query an indexed transaction and print its decoded MsgWithdrawResponse.
The transaction must be included in a block and have execution code zero.
Use --msg-index to select a top-level message when the transaction contains
multiple messages. This command does not broadcast or wait for inclusion.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hash, err := hex.DecodeString(args[0])
			if err != nil || len(hash) != tmhash.Size {
				return fmt.Errorf("tx-hash must be a %d-character hexadecimal transaction hash", tmhash.Size*2)
			}
			msgIndex, err := cmd.Flags().GetUint32("msg-index")
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			node, err := clientCtx.GetNode()
			if err != nil {
				return err
			}
			result, err := node.Tx(cmd.Context(), hash, false)
			if err != nil {
				return fmt.Errorf("query committed transaction: %w", err)
			}
			if result == nil || result.Height <= 0 {
				return fmt.Errorf("transaction has not been included in a block")
			}
			if result.TxResult.Code != 0 {
				return fmt.Errorf("transaction failed with code %d (%s): %s", result.TxResult.Code, result.TxResult.Codespace, result.TxResult.Log)
			}
			var msgData sdk.TxMsgData
			if err := msgData.Unmarshal(result.TxResult.Data); err != nil {
				return fmt.Errorf("decode transaction message responses: %w", err)
			}
			if uint64(msgIndex) >= uint64(len(msgData.MsgResponses)) {
				return fmt.Errorf("message index %d out of range: transaction has %d message responses", msgIndex, len(msgData.MsgResponses))
			}
			response := msgData.MsgResponses[msgIndex]
			expectedType := sdk.MsgTypeURL(&types.MsgWithdrawResponse{})
			if response == nil || response.TypeUrl != expectedType {
				return fmt.Errorf("message response %d is not %s", msgIndex, expectedType)
			}
			var withdrawal types.MsgWithdrawResponse
			if err := withdrawal.Unmarshal(response.Value); err != nil {
				return fmt.Errorf("decode withdrawal response: %w", err)
			}
			return clientCtx.PrintProto(&withdrawal)
		},
	}
	cmd.Flags().Uint32("msg-index", 0, "Zero-based index of the top-level withdrawal message")
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
