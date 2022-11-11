package main

import (
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/viper"

	dscApi "bitbucket.org/decimalteam/dsc-go-sdk/api"
	dscTx "bitbucket.org/decimalteam/dsc-go-sdk/tx"
	dscWallet "bitbucket.org/decimalteam/dsc-go-sdk/wallet"
)

func main() {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForValidator("dxvaloper", "dxvaloperpub")

	viper.SetConfigFile(".env")
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Printf("viper.ReadInConfig error: %s", err.Error())
		return
	}
	dscGateway := viper.GetString("DECIMAL_GATEWAY")
	dscNode := viper.GetString("DECIMAL_NODE")
	mnemonic := viper.GetString("MNEMONIC")

	api := dscApi.NewAPI(dscGateway, dscNode)

	acc, err := dscWallet.NewAccountFromMnemonicWords(mnemonic, "")
	if err != nil {
		fmt.Printf("can't get create account from mnemonic: %s\n", err.Error())
		return
	}
	err = api.GetParameters()
	if err != nil {
		fmt.Printf("can't get parameters from to DecimalGateway: %s\n", err.Error())
		return
	}
	num, seq, err := api.GetAccountNumberAndSequence(acc.Address())
	if err != nil {
		fmt.Printf("can't get account number and sequence from to DecimalGateway: %s\n", err.Error())
		return
	}
	acc = acc.WithChainID(api.ChainID())
	acc = acc.WithAccountNumber(num)
	acc = acc.WithSequence(seq)

	msg := dscTx.NewMsgSetOffline(sdk.ValAddress(acc.SdkAddress()))
	tx, err := dscTx.BuildTransaction(acc, []sdk.Msg{msg}, "", sdk.NewCoin(api.BaseCoin(), sdk.NewInt(0)))
	if err != nil {
		fmt.Printf("can't build Setoffline transaction: %s\n", err.Error())
		return
	}
	err = tx.SignTransaction(acc)
	if err != nil {
		fmt.Printf("can't sign transaction: %s\n", err.Error())
		return
	}
	bz, err := tx.BytesToSend()
	if err != nil {
		fmt.Printf("can't built transaction bytes: %s\n", err.Error())
		return
	}
	fmt.Println(hex.EncodeToString(bz))
}
