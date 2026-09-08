package ledger

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hyperledger/fabric-gateway/pkg/client"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/msp"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"google.golang.org/protobuf/proto"
)

// ErrOffline is returned when no Fabric network is configured or reachable.
var ErrOffline = errors.New("the ledger is offline")

// Client is what the service needs from the ledger. *Fabric implements it; Offline
// answers ErrOffline so the platform keeps working (records wait to be sealed).
type Client interface {
	SealBatch(in SealInput) (*SealOnChain, string, error)
	GetSeal(id string) (*SealOnChain, error)
	RegisterSigner(in SignerInput) (*SignerOnChain, string, error)
	RevokeSigner(keyID, reason string) (*SignerOnChain, string, error)
	GetSigner(keyID string) (*SignerOnChain, error)
	ChainInfo() (*ChainInfo, error)
	Block(n uint64) (*Block, error)
	BlockByTx(txID string) (*Block, error)
	Channel() string
}

// SealInput mirrors the chaincode's SealInput.
type SealInput struct {
	SealID     string   `json:"sealId"`
	MerkleRoot string   `json:"merkleRoot"`
	Leaves     []string `json:"leaves"`
	FirstSeq   int64    `json:"firstSeq"`
	LastSeq    int64    `json:"lastSeq"`
	Provenance string   `json:"provenance"`
}

// SealOnChain mirrors the chaincode's Seal.
type SealOnChain struct {
	SealID       string `json:"sealId"`
	MerkleRoot   string `json:"merkleRoot"`
	LeafCount    int    `json:"leafCount"`
	FirstSeq     int64  `json:"firstSeq"`
	LastSeq      int64  `json:"lastSeq"`
	Provenance   string `json:"provenance"`
	SubmitterMSP string `json:"submitterMsp"`
	TxID         string `json:"txId"`
	SealedAt     string `json:"sealedAt"`
}

// SignerInput mirrors the chaincode's SignerInput.
type SignerInput struct {
	KeyID     string `json:"keyId"`
	OrgID     string `json:"orgId"`
	OrgName   string `json:"orgName"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"publicKey"`
	HolderID  string `json:"holderId,omitempty"` // pseudonymous user id of a personal key
}

// SignerOnChain mirrors the chaincode's Signer.
type SignerOnChain struct {
	KeyID        string `json:"keyId"`
	OrgID        string `json:"orgId"`
	OrgName      string `json:"orgName"`
	HolderID     string `json:"holderId,omitempty"`
	Algorithm    string `json:"algorithm"`
	PublicKey    string `json:"publicKey"`
	Status       string `json:"status"`
	RevokeReason string `json:"revokeReason,omitempty"`
	RegisteredAt string `json:"registeredAt"`
	RevokedAt    string `json:"revokedAt,omitempty"`
	TxID         string `json:"txId"`
}

// ChainInfo is the channel's height and head.
type ChainInfo struct {
	Channel           string `json:"channel"`
	Height            uint64 `json:"height"`
	CurrentBlockHash  string `json:"currentBlockHash"`
	PreviousBlockHash string `json:"previousBlockHash"`
}

// Tx is one transaction inside a block.
type Tx struct {
	TxID       string    `json:"txId"`
	Timestamp  time.Time `json:"timestamp"`
	Type       string    `json:"type"`
	CreatorMSP string    `json:"creatorMsp"`
	Chaincode  string    `json:"chaincode,omitempty"`
	Function   string    `json:"function,omitempty"`
	Subject    string    `json:"subject,omitempty"` // seal id or key id
	Valid      bool      `json:"valid"`
	Validation string    `json:"validation"`
}

// Block is a decoded block header with its transactions.
type Block struct {
	Number       uint64 `json:"number"`
	DataHash     string `json:"dataHash"`
	PreviousHash string `json:"previousHash"`
	Txs          []Tx   `json:"txs"`
	SizeBytes    int    `json:"sizeBytes"`
}

// Fabric is a live gateway client. Sessions are opened lazily and reopened after
// failures, so a peer restart does not need a service restart.
type Fabric struct {
	dir, org, peer, channel string

	mu sync.Mutex
	s  *Session
}

// NewFabric returns a client for the operator identity in dir (fabric/).
func NewFabric(dir, org, peerAddr, channel string) *Fabric {
	return &Fabric{dir: dir, org: org, peer: peerAddr, channel: channel}
}

// Channel returns the channel name.
func (f *Fabric) Channel() string { return f.channel }

func (f *Fabric) session() (*Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.s != nil {
		return f.s, nil
	}
	if f.peer != "" {
		o := Orgs[f.org]
		o.Peer = f.peer
		Orgs[f.org] = o
	}
	s, err := Connect(f.dir, f.org, "")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOffline, err)
	}
	f.s = s
	return s, nil
}

func (f *Fabric) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.s != nil {
		f.s.Close()
		f.s = nil
	}
}

func (f *Fabric) submit(fn string, out any, args ...string) (string, error) {
	s, err := f.session()
	if err != nil {
		return "", err
	}
	c := s.Contract(f.channel)
	p, err := c.NewProposal(fn, withArgs(args...))
	if err != nil {
		return "", err
	}
	txID := p.TransactionID()
	tx, err := p.Endorse()
	if err != nil {
		f.resetOnTransport(err)
		return txID, errors.New(ErrText(err))
	}
	commit, err := tx.Submit()
	if err != nil {
		f.resetOnTransport(err)
		return txID, errors.New(ErrText(err))
	}
	st, err := commit.Status()
	if err != nil {
		return txID, err
	}
	if !st.Successful {
		return txID, fmt.Errorf("transaction %s did not commit: %s", txID, st.Code)
	}
	if out != nil {
		if err := json.Unmarshal(tx.Result(), out); err != nil {
			return txID, err
		}
	}
	return txID, nil
}

func (f *Fabric) evaluate(cc, fn string, args ...string) ([]byte, error) {
	s, err := f.session()
	if err != nil {
		return nil, err
	}
	out, err := s.gw.GetNetwork(f.channel).GetContract(cc).EvaluateTransaction(fn, args...)
	if err != nil {
		f.resetOnTransport(err)
		return nil, errors.New(ErrText(err))
	}
	return out, nil
}

func (f *Fabric) resetOnTransport(err error) {
	if strings.Contains(err.Error(), "Unavailable") || strings.Contains(err.Error(), "connection") {
		f.reset()
	}
}

// SealBatch anchors a Merkle root.
func (f *Fabric) SealBatch(in SealInput) (*SealOnChain, string, error) {
	b, _ := json.Marshal(in)
	var out SealOnChain
	tx, err := f.submit("SealBatch", &out, string(b))
	return &out, tx, err
}

// GetSeal reads a seal.
func (f *Fabric) GetSeal(id string) (*SealOnChain, error) {
	b, err := f.evaluate("roadpass-proof", "GetSeal", id)
	if err != nil {
		return nil, err
	}
	var s SealOnChain
	return &s, json.Unmarshal(b, &s)
}

// RegisterSigner puts a public key on the ledger.
func (f *Fabric) RegisterSigner(in SignerInput) (*SignerOnChain, string, error) {
	b, _ := json.Marshal(in)
	var out SignerOnChain
	tx, err := f.submit("RegisterSigner", &out, string(b))
	return &out, tx, err
}

// RevokeSigner revokes a key.
func (f *Fabric) RevokeSigner(keyID, reason string) (*SignerOnChain, string, error) {
	var out SignerOnChain
	tx, err := f.submit("RevokeSigner", &out, keyID, reason)
	return &out, tx, err
}

// GetSigner reads a key.
func (f *Fabric) GetSigner(keyID string) (*SignerOnChain, error) {
	b, err := f.evaluate("roadpass-proof", "GetSigner", keyID)
	if err != nil {
		return nil, err
	}
	var s SignerOnChain
	return &s, json.Unmarshal(b, &s)
}

// ChainInfo reads the channel height through qscc.
func (f *Fabric) ChainInfo() (*ChainInfo, error) {
	b, err := f.evaluate("qscc", "GetChainInfo", f.channel)
	if err != nil {
		return nil, err
	}
	var info common.BlockchainInfo
	if err := proto.Unmarshal(b, &info); err != nil {
		return nil, err
	}
	return &ChainInfo{Channel: f.channel, Height: info.GetHeight(),
		CurrentBlockHash: hex.EncodeToString(info.GetCurrentBlockHash()), PreviousBlockHash: hex.EncodeToString(info.GetPreviousBlockHash())}, nil
}

// Block reads and decodes block n.
func (f *Fabric) Block(n uint64) (*Block, error) {
	b, err := f.evaluate("qscc", "GetBlockByNumber", f.channel, fmt.Sprint(n))
	if err != nil {
		return nil, err
	}
	return decodeBlock(b)
}

// BlockByTx reads the block containing a transaction.
func (f *Fabric) BlockByTx(txID string) (*Block, error) {
	b, err := f.evaluate("qscc", "GetBlockByTxID", f.channel, txID)
	if err != nil {
		return nil, err
	}
	return decodeBlock(b)
}

func decodeBlock(raw []byte) (*Block, error) {
	var blk common.Block
	if err := proto.Unmarshal(raw, &blk); err != nil {
		return nil, err
	}
	h := blk.GetHeader()
	out := &Block{Number: h.GetNumber(), DataHash: hex.EncodeToString(h.GetDataHash()),
		PreviousHash: hex.EncodeToString(h.GetPreviousHash()), SizeBytes: len(raw)}
	var filter []byte
	if md := blk.GetMetadata().GetMetadata(); len(md) > int(common.BlockMetadataIndex_TRANSACTIONS_FILTER) {
		filter = md[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
	}
	for i, envBytes := range blk.GetData().GetData() {
		tx := decodeTx(envBytes)
		code := peer.TxValidationCode_VALID
		if i < len(filter) {
			code = peer.TxValidationCode(filter[i])
		}
		tx.Valid, tx.Validation = code == peer.TxValidationCode_VALID, code.String()
		out.Txs = append(out.Txs, tx)
	}
	return out, nil
}

func decodeTx(envBytes []byte) Tx {
	var tx Tx
	var env common.Envelope
	if proto.Unmarshal(envBytes, &env) != nil {
		return tx
	}
	var pl common.Payload
	if proto.Unmarshal(env.GetPayload(), &pl) != nil {
		return tx
	}
	var ch common.ChannelHeader
	if proto.Unmarshal(pl.GetHeader().GetChannelHeader(), &ch) == nil {
		tx.TxID = ch.GetTxId()
		tx.Timestamp = ch.GetTimestamp().AsTime().UTC()
		tx.Type = common.HeaderType(ch.GetType()).String()
	}
	var sh common.SignatureHeader
	if proto.Unmarshal(pl.GetHeader().GetSignatureHeader(), &sh) == nil {
		var id msp.SerializedIdentity
		if proto.Unmarshal(sh.GetCreator(), &id) == nil {
			tx.CreatorMSP = id.GetMspid()
		}
	}
	if common.HeaderType(ch.GetType()) != common.HeaderType_ENDORSER_TRANSACTION {
		return tx
	}
	var t peer.Transaction
	if proto.Unmarshal(pl.GetData(), &t) != nil || len(t.GetActions()) == 0 {
		return tx
	}
	var ap peer.ChaincodeActionPayload
	if proto.Unmarshal(t.GetActions()[0].GetPayload(), &ap) != nil {
		return tx
	}
	var cpp peer.ChaincodeProposalPayload
	if proto.Unmarshal(ap.GetChaincodeProposalPayload(), &cpp) != nil {
		return tx
	}
	var spec peer.ChaincodeInvocationSpec
	if proto.Unmarshal(cpp.GetInput(), &spec) != nil {
		return tx
	}
	tx.Chaincode = spec.GetChaincodeSpec().GetChaincodeId().GetName()
	args := spec.GetChaincodeSpec().GetInput().GetArgs()
	if len(args) > 0 {
		tx.Function = string(args[0])
		if i := strings.LastIndexByte(tx.Function, ':'); i >= 0 {
			tx.Function = tx.Function[i+1:]
		}
	}
	if len(args) > 1 {
		var m map[string]any
		if json.Unmarshal(args[1], &m) == nil {
			for _, k := range []string{"sealId", "keyId", "importBatchId", "packageId", "recordId"} {
				if v, ok := m[k].(string); ok {
					tx.Subject = v
					break
				}
			}
		} else if len(args[1]) < 200 {
			tx.Subject = string(args[1])
		}
	}
	return tx
}

// Offline is the ledger when Fabric is not configured.
type Offline struct{}

func (Offline) SealBatch(SealInput) (*SealOnChain, string, error) { return nil, "", ErrOffline }
func (Offline) GetSeal(string) (*SealOnChain, error)              { return nil, ErrOffline }
func (Offline) RegisterSigner(SignerInput) (*SignerOnChain, string, error) {
	return nil, "", ErrOffline
}
func (Offline) RevokeSigner(string, string) (*SignerOnChain, string, error) {
	return nil, "", ErrOffline
}
func (Offline) GetSigner(string) (*SignerOnChain, error) { return nil, ErrOffline }
func (Offline) ChainInfo() (*ChainInfo, error)           { return nil, ErrOffline }
func (Offline) Block(uint64) (*Block, error)             { return nil, ErrOffline }
func (Offline) BlockByTx(string) (*Block, error)         { return nil, ErrOffline }
func (Offline) Channel() string                          { return "" }

func withArgs(args ...string) client.ProposalOption { return client.WithArguments(args...) }
