package profiles

import (
	"crypto/rand"
	"fmt"
)

func SnapshotOf(p Profile) Snapshot {
	return Snapshot{ProfileID: p.ID, ProfileRevision: p.Revision, TypeID: p.TypeID, TypeVersion: p.TypeVersion, Config: CloneObject(p.Config)}
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
