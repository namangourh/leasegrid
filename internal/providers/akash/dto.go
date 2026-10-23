package akash

// The types in this file mirror the parts of Akash's REST responses that
// LeaseGrid reads, as observed on api.akashnet.net in October 2026
// (market/v1beta5 and deployment/v1beta4). Fields LeaseGrid doesn't need are
// left out: encoding/json ignores unknown fields, so fields Akash adds later
// don't break decoding. None of these types leave this package.

// leaseListResponse is the body of GET /akash/market/v1beta5/leases/list.
type leaseListResponse struct {
	Leases     []leaseEntry `json:"leases"`
	Pagination pagination   `json:"pagination"`
}

// pagination tells the client how to fetch the next page.
type pagination struct {
	NextKey string `json:"next_key"` // empty (or null) on the last page
}

type leaseEntry struct {
	Lease leaseDTO `json:"lease"`
}

type leaseDTO struct {
	ID    leaseID `json:"id"`
	State string  `json:"state"`
	Price coin    `json:"price"` // per block
}

type leaseID struct {
	Owner    string `json:"owner"`
	DSeq     string `json:"dseq"`     // deployment sequence, sent as a string
	GSeq     uint32 `json:"gseq"`     // group sequence
	OSeq     uint32 `json:"oseq"`     // order sequence
	Provider string `json:"provider"` // the hosting provider's address
}

// coin is a Cosmos amount. The amount is a decimal string, so no precision
// is lost in transit.
type coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// deploymentResponse is the body of
// GET /akash/deployment/v1beta4/deployments/info.
type deploymentResponse struct {
	Groups        []group       `json:"groups"`
	EscrowAccount escrowAccount `json:"escrow_account"`
}

type group struct {
	ID        groupID   `json:"id"`
	GroupSpec groupSpec `json:"group_spec"`
}

type groupID struct {
	GSeq uint32 `json:"gseq"`
}

type groupSpec struct {
	Name      string          `json:"name"`
	Resources []groupResource `json:"resources"`
}

// groupResource is one resource profile and how many instances of it run.
type groupResource struct {
	Resource resourceUnits `json:"resource"`
	Count    uint32        `json:"count"`
}

type resourceUnits struct {
	CPU     cpuUnits      `json:"cpu"`
	Memory  memoryUnits   `json:"memory"`
	Storage []storageUnit `json:"storage"`
	GPU     gpuUnits      `json:"gpu"`
}

type cpuUnits struct {
	Units quantity `json:"units"` // millicores
}

type memoryUnits struct {
	Quantity quantity `json:"quantity"` // bytes
}

type storageUnit struct {
	Quantity quantity `json:"quantity"` // bytes
}

type gpuUnits struct {
	Units quantity `json:"units"`
}

// quantity is an integer sent as a string, e.g. {"val": "536870912"}.
type quantity struct {
	Val string `json:"val"`
}

type escrowAccount struct {
	State escrowState `json:"state"`
}

type escrowState struct {
	Funds []coin `json:"funds"` // what's left to pay providers with
}
