package server

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRecurringTriggerDoesNotRecursivelyAcquireOperationGate(t *testing.T) {
	gate := &sync.RWMutex{}
	enteredHandler := make(chan struct{})
	allowServiceLock := make(chan struct{})
	serviceLockAcquired := make(chan struct{})
	releaseServiceLock := make(chan struct{})
	requestDone := make(chan struct{})

	inner := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(enteredHandler)
		<-allowServiceLock
		gate.RLock() // service-level protection used by recurring background and HTTP calls
		close(serviceLockAcquired)
		<-releaseServiceLock
		gate.RUnlock()
	})
	handler := DatabaseOperationMiddleware(gate, inner)
	req := httptest.NewRequest(http.MethodPost, "/api/recurring-schedules/trigger", nil)

	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), req)
		close(requestDone)
	}()

	select {
	case <-enteredHandler:
	case <-time.After(time.Second):
		t.Fatal("request recurring tidak mencapai handler")
	}

	writerAcquired := make(chan struct{})
	releaseWriter := make(chan struct{})
	go func() {
		gate.Lock()
		close(writerAcquired)
		<-releaseWriter
		gate.Unlock()
	}()

	select {
	case <-writerAcquired:
		// Benar: endpoint recurring tidak memegang middleware RLock; restore dapat memperoleh writer lock.
	case <-time.After(500 * time.Millisecond):
		// Cleanup jalur regresi lama: lepaskan RLock middleware secara terkontrol agar goroutine tidak bocor.
		close(allowServiceLock)
		gate.RUnlock()
		<-writerAcquired
		close(releaseWriter)
		<-serviceLockAcquired
		gate.RLock()
		close(releaseServiceLock)
		<-requestDone
		t.Fatal("middleware masih memegang RLock untuk recurring trigger; interleaving restore dapat deadlock")
	}

	close(allowServiceLock)
	select {
	case <-serviceLockAcquired:
		t.Fatal("service-level RLock tidak boleh melewati restore writer lock")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseWriter)
	select {
	case <-serviceLockAcquired:
	case <-time.After(time.Second):
		t.Fatal("service recurring tidak melanjutkan setelah restore melepas writer lock")
	}
	close(releaseServiceLock)

	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("request recurring tidak selesai setelah operation gate dilepas")
	}
}
