// Package traffic carries passive copies of inbound TCP bytes, independent of telemetry.
package traffic

import (
	"sync"

	"github.com/gin-gonic/gin/binding"

	"honey-forge/internal/contract"
)

const MaxChunkBytes = 8192

const QueueCapacity = 64

type Frame struct {
	ConnectionID  string `json:"connection_id" validate:"required,uuid"`
	Sequence      int64  `json:"sequence" validate:"min=1"`
	Kind          string `json:"kind" validate:"oneof=open data close"`
	RemoteAddress string `json:"remote_address" validate:"required,max=255"`
	LocalAddress  string `json:"local_address" validate:"required,max=255"`
	Data          []byte `json:"data,omitempty" validate:"required_if=Kind data,excluded_unless=Kind data,omitnil,min=1,max=8192"`
}

func CheckFrame(f Frame) error {
	contract.Configure()
	if err := binding.Validator.ValidateStruct(f); err != nil {
		return contract.NewError("validation_failed")
	}

	return nil
}

type Delivery struct {
	SourceID string `json:"source_id" validate:"required,uuid"`
	TargetID string `json:"target_id" validate:"required,uuid"`
	Frame    `json:"frame"`
}

// Inbox retains the most recent passive copies. It never feeds a listener or
// re-publishes traffic, so even directed cycles cannot create forwarding loops.
type Inbox struct {
	mu       sync.Mutex // protects frames and dropped
	frames   []Delivery
	capacity int
	dropped  uint64
}

func NewInbox(capacity int) *Inbox {
	if capacity < 1 {
		capacity = QueueCapacity
	}

	return &Inbox{capacity: capacity}
}

func copyDelivery(d Delivery) Delivery {
	d.Data = append([]byte(nil), d.Data...)
	return d
}

func (i *Inbox) Receive(d Delivery) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if len(i.frames) == i.capacity {
		copy(i.frames, i.frames[1:])
		i.frames = i.frames[:len(i.frames)-1]
		i.dropped++
	}

	i.frames = append(i.frames, copyDelivery(d))
}

func (i *Inbox) Snapshot() ([]Delivery, uint64) {
	i.mu.Lock()
	defer i.mu.Unlock()

	frames := make([]Delivery, len(i.frames))
	for n, frame := range i.frames {
		frames[n] = copyDelivery(frame)
	}

	return frames, i.dropped
}
