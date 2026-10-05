package audiohub

import (
	"sync"
	"testing"
)

func TestDeletedRoomReleasesAudioAndCannotRejoin(t *testing.T) {
	h := New()
	ch, _, cancel := h.Subscribe(15)
	defer cancel()
	controls, cancelControls := h.SubscribeControls(15)
	defer cancelControls()
	if _, err := h.RegisterReceiver(Receiver{ReceiverID: "box-a", RoomID: 15}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RegisterReceiver(Receiver{ReceiverID: "box-b", RoomID: 16}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Publish(Task{ID: "task-15", RoomID: 15, AudioURL: "https://example.test/audio.wav", DurationMS: 60000}); err != nil {
		t.Fatal(err)
	}
	<-ch
	h.ClearRoom(15)
	h.ClearRoom(15)
	if _, ok := <-ch; ok {
		t.Fatal("task subscribers remain open")
	}
	if _, ok := <-controls; ok {
		t.Fatal("control subscribers remain open")
	}
	if h.ActiveTask(15) != nil {
		t.Fatal("active task leaked")
	}
	if _, ok := h.Snapshot("task-15"); ok {
		t.Fatal("task buffer leaked")
	}
	if len(h.RoomReceivers(15)) != 0 || len(h.RoomReceivers(16)) != 1 {
		t.Fatal("receiver cleanup was not room scoped")
	}
	if _, err := h.RegisterReceiver(Receiver{ReceiverID: "box-a", RoomID: 15}); err == nil {
		t.Fatal("deleted room accepted stale heartbeat registration")
	}
	if _, err := h.Publish(Task{ID: "late", RoomID: 15, AudioURL: "https://example.test/audio.wav", DurationMS: 60000}); err == nil {
		t.Fatal("in-flight producer recreated deleted room")
	}
	late, _, cancelLate := h.Subscribe(15)
	defer cancelLate()
	if _, ok := <-late; ok {
		t.Fatal("late stream did not close")
	}
}

func TestClearRoomConcurrentWithControlBroadcast(t *testing.T) {
	h := New()
	_, cancel := h.SubscribeControls(15)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			h.BroadcastControl(ControlEvent{RoomID: 15, Action: "stop"})
		}
	}()
	h.ClearRoom(15)
	wg.Wait()
}
