package traffic

import (
	"bytes"
	"io"
	"net"
	"testing"
)

func TestCopiesUnprocessedBytesAndPreservesRead(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	var frames []Frame
	conn := wrapConn(server, func(frame Frame) { frames = append(frames, frame) })
	input := []byte{0, 255, 13, 10, 1}
	done := make(chan error, 1)
	go func() { _, err := client.Write(input); done <- err }()
	buffer := make([]byte, len(input))
	if _, err := io.ReadFull(conn, buffer); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buffer, input) {
		t.Fatal("original bytes changed")
	}
	clear(buffer)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 || frames[0].Kind != "open" || frames[1].Kind != "data" || frames[2].Kind != "close" {
		t.Fatal(frames)
	}
	if !bytes.Equal(frames[1].Data, input) {
		t.Fatal("copy aliases read buffer")
	}
	for i, frame := range frames {
		if frame.Sequence != int64(i+1) || frame.ConnectionID != frames[0].ConnectionID {
			t.Fatal(frame)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatal("duplicate close")
	}
}

func TestInboxBoundedAndCopiesData(t *testing.T) {
	inbox := NewInbox(2)
	for i := int64(1); i <= 3; i++ {
		inbox.Receive(Delivery{Frame: Frame{Sequence: i, Data: []byte{byte(i)}}})
	}
	got, dropped := inbox.Snapshot()
	if len(got) != 2 || dropped != 1 || got[0].Sequence != 2 {
		t.Fatalf("%+v, dropped=%d", got, dropped)
	}
	got[0].Data[0] = 99
	again, _ := inbox.Snapshot()
	if again[0].Data[0] != 2 {
		t.Fatal("snapshot aliases inbox")
	}
}
