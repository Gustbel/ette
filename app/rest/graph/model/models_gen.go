// Code generated by github.com/99designs/gqlgen, DO NOT EDIT.

package model

type Block struct {
	Hash            string  `json:"hash"`
	Number          string  `json:"number"`
	Time            string  `json:"time"`
	ParentHash      string  `json:"parentHash"`
	Difficulty      string  `json:"difficulty"`
	GasUsed         string  `json:"gasUsed"`
	GasLimit        string  `json:"gasLimit"`
	Nonce           string  `json:"nonce"`
	Miner           string  `json:"miner"`
	Size            float64 `json:"size"`
	TxRootHash      string  `json:"txRootHash"`
	ReceiptRootHash string  `json:"receiptRootHash"`
}

type Event struct {
	Origin    string   `json:"origin"`
	Index     string   `json:"index"`
	Topics    []string `json:"topics"`
	Data      string   `json:"data"`
	TxHash    string   `json:"txHash"`
	BlockHash string   `json:"blockHash"`
}

type Transaction struct {
	Hash      string `json:"hash"`
	From      string `json:"from"`
	To        string `json:"to"`
	Contract  string `json:"contract"`
	Value     string `json:"value"`
	Data      string `json:"data"`
	Gas       string `json:"gas"`
	GasPrice  string `json:"gasPrice"`
	Cost      string `json:"cost"`
	Nonce     string `json:"nonce"`
	State     string `json:"state"`
	BlockHash string `json:"blockHash"`
}
