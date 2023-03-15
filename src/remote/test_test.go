// summary of tests
// -- TestCheckpoint_ServiceInterface: Service rejects nils and non-remote interfaces
// -- TestCheckpoint_ServiceRemote: Service can be started, accept connections, and stopped
// -- TestFinal_StubInterface: StubFactory rejects nils and non-remote interfaces
// -- TestFinal_StubConnects: Stub can connect to given address
// -- TestFinal_Connection: verifies argument passing, transmission of return values and remote exceptions
// -- TestFinal_LossyConnection: runs many calls over unreliable channel to ensure errors are handled
// -- TestFinal_Multithread: checks that the service supports multiple simultaneous connections
// -- TestFinal_Mismatch: checks error handling for mismatched interfaces in stub and service
package remote

import (
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// simple interface for a remote service
type SimpleInterface struct {
	// test method for sending arguments, return values, and errors.
	// @param value -- an integer value
	// @param return_error -- boolean value indicating an error string request
	// @return if return_error is true, returns -value and an Error, otherwise value and ""
	Method func(int, bool) (int, string, RemoteObjectError)
	// method used to coordinate concurrent calls to the same Service.
	Rendezvous func() RemoteObjectError
}

// service object implementing the SimpleInterface methods
type SimpleObject struct {
	mu sync.Mutex
	// set by first call, then wait until second call.
	wake bool
	// use a WaitGroup to synchronize two threads
	wg sync.WaitGroup
}

// Service implementation of the Method method in SimpleInterface
func (obj *SimpleObject) Method(value int, return_error bool) (int, string, RemoteObjectError) {
	if return_error {
		err := "This is an error."
		return -value, err, RemoteObjectError{}
	}
	return value, "", RemoteObjectError{}
}

// Service implementation of the Rendezvous method in SimpleInterface
func (obj *SimpleObject) Rendezvous() RemoteObjectError {
	var w bool
	obj.mu.Lock()
	w = obj.wake
	obj.mu.Unlock()
	if !w {
		obj.mu.Lock()
		obj.wake = true
		obj.mu.Unlock()
		obj.wg.Add(1)

		obj.wg.Wait()
	} else {
		obj.wg.Done()
	}
	return RemoteObjectError{}
}

// Service implementation of (non-remote) Wake method in SimpleObject
func (obj *SimpleObject) Wake() {
	obj.wg.Done()
}

// non-remote interface for error checking
type BadInterface struct {
	// test method that doesn't return a RemoteObjectError, should be rejected by Stub and Service.
	Method func(int, bool) (int, string)
}

// service object implementing the BadInterface methods
type BadObject struct{}

// Service implementation of the Method method in BadInterface
func (obj *BadObject) Method(value int, return_error bool) (int, string) {
	if return_error {
		err := "This is an error."
		return -value, err
	}
	return value, ""
}

// mismatched interface to test error handling at stub
type MismatchInterface struct {
	// mismatched method with (int, int) instead of (int, bool) parameters
	Method func(int, int) (int, string, RemoteObjectError)
	// mismatched method with (int, RemoteObjectError) instead of RemoteObjectError return type
	Rendezvous func() (int, RemoteObjectError)
	// extra method not included in SimpleService interface
	ExtraMethod func() RemoteObjectError
}

// another mismatched interface to test error handling at stub
type MismatchInterface2 struct {
	// mismatched method with (int, bool, int) instead of (int, bool) parameters
	Method func(int, bool, int) (int, string, RemoteObjectError)
	// mismatched method with (int, RemoteObjectError) instead of RemoteObjectError return type
	Rendezvous func() RemoteObjectError
}

// helper function for testing whether listening socket is active
func probe(port int) bool {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// TestCheckpoint_ServiceInterface -- perform basic tests on Service creation
// -- verifies that MakeService rejects non-remote interfaces and nulls
// -- verifies that MakeService accepts remote interfaces
func TestCheckpoint_ServiceInterface(t *testing.T) {

	// choose a large-ish random port number for each test
	port := rand.Intn(10000) + 7000

	// should throw an error due to non-remote interface definition and instance
	_, err := NewService(&BadInterface{}, &BadObject{}, port, false, false)
	if err == nil {
		t.Fatalf("NewService accepted non-remote service interface and instance")
	}

	// should throw an error due to nil interface
	_, err = NewService(nil, &SimpleObject{}, port, false, false)
	if err == nil {
		t.Fatalf("NewService accepted nil service interface")
	}

	// should throw an error due to nil instance
	_, err = NewService(&SimpleInterface{}, nil, port, false, false)
	if err == nil {
		t.Fatalf("NewService accepted nil service instance")
	}

	// should work correctly with no error
	_, err = NewService(&SimpleInterface{}, &SimpleObject{}, port, false, false)
	if err != nil {
		t.Fatalf("NewService did not accept proper service interface and instance")
	}
}

// TestCheckpoint_ServiceRuns -- perform basic tests on Service running
// -- verifies that Service can be started, stopped, and connected to
func TestCheckpoint_ServiceRuns(t *testing.T) {

	// choose a large-ish random port number for each test
	port := rand.Intn(10000) + 7000

	// create the service, should work if the previous test passed
	srvc, err := NewService(&SimpleInterface{}, &SimpleObject{}, port, false, false)
	if err != nil {
		t.Fatalf("Error in NewService: %s", err.Error())
	}
	if srvc == nil {
		t.Fatalf("NewService returned nil service")
	}

	if probe(port) {
		t.Fatalf("Service accepts connections before start")
	}

	err = srvc.Start()
	if err != nil {
		t.Fatalf("Error in Service.start(): %s", err.Error())
	}

	// wait for service to start...or timeout
	ddln := time.Now().Add(5 * time.Second)
	for !srvc.IsRunning() && time.Now().Before(ddln) {
	}
	if !srvc.IsRunning() {
		t.Fatalf("Timeout waiting for Service to start")
	}

	if !probe(port) {
		t.Fatalf("Service refuses connections after start")
	}

	srvc.Stop()

	// wait for service to stop...or timeout
	ddln = time.Now().Add(5 * time.Second)
	for srvc.IsRunning() && time.Now().Before(ddln) {
	}
	if srvc.IsRunning() {
		t.Fatalf("Timeout waiting for Service to stop")
	}

	if probe(port) {
		t.Fatalf("Service accepts connections after stop")
	}
}

// TestFinal_StubInterface -- perform basic tests on Stub creation
// -- verifies that StubFactory rejects non-remote interfaces and nulls
// -- verifies that StubFactory accepts remote interfaces
func TestFinal_StubInterface(t *testing.T) {

	// choose a large-ish random port number for each test
	port := rand.Intn(10000) + 7000
	addr := "127.0.0.1:" + strconv.Itoa(port)

	// should throw an error due to non-remote interface definition and instance
	err := StubFactory(&BadInterface{}, addr, false, false)
	if err == nil {
		t.Fatalf("StubFactory accepted non-remote service interface")
	}

	// should throw an error due to nil interface
	err = StubFactory(nil, addr, false, false)
	if err == nil {
		t.Fatalf("StubFactory accepted nil service interface")
	}

	// should work correctly with no error
	stub := &SimpleInterface{}
	err = StubFactory(stub, addr, false, false)
	if err != nil {
		t.Fatalf("StubFactory failed to create service proxy from interface")
	}
	// TODO: what to check here?
}

// TestFinal_StubConnects -- perform basic tests on Stub use, namely
// verifies that a stub can actually connect to a Service at the given address.
func TestFinal_StubConnects(t *testing.T) {

	// choose a large-ish random port number for each test
	port := rand.Intn(10000) + 7000
	addr := "127.0.0.1:" + strconv.Itoa(port)

	// create the service, should work if the previous test passed
	srvc, err := NewService(&SimpleInterface{}, &SimpleObject{}, port, false, false)
	if err != nil {
		t.Fatalf("Error in NewService: %s", err.Error())
	}
	if srvc == nil {
		t.Fatalf("NewService returned nil service")
	}

	err = srvc.Start()
	if err != nil {
		t.Fatalf("Error in Service.start(): %s", err.Error())
	}
	defer srvc.Stop()

	// wait for service to start...or timeout
	ddln := time.Now().Add(5 * time.Second)
	for !srvc.IsRunning() && time.Now().Before(ddln) {
	}
	if !srvc.IsRunning() {
		t.Fatalf("Timeout waiting for Service to start")
	}

	// create the stub, should work if previous test passed
	stub := &SimpleInterface{}
	err = StubFactory(stub, addr, false, false)
	if err != nil {
		t.Fatalf("StubFactory failed to create service proxy from interface")
	}

	stub.Method(1, false)
}

// TestFinal_Connection -- tests complete connection between stub and service,
// including passing arguments, return values, and remote exceptions.
func TestFinal_Connection(t *testing.T) {

	// choose a large-ish random port number for each test
	port := rand.Intn(10000) + 7000
	addr := "127.0.0.1:" + strconv.Itoa(port)

	// create the service, should work if the previous test passed
	srvc, err := NewService(&SimpleInterface{}, &SimpleObject{}, port, false, false)
	if err != nil {
		t.Fatalf("Error in NewService: %s", err.Error())
	}
	if srvc == nil {
		t.Fatalf("NewService returned nil service")
	}

	err = srvc.Start()
	if err != nil {
		t.Fatalf("Error in Service.start(): %s", err.Error())
	}
	defer srvc.Stop()

	// wait for service to start...or timeout
	ddln := time.Now().Add(5 * time.Second)
	for !srvc.IsRunning() && time.Now().Before(ddln) {
	}
	if !srvc.IsRunning() {
		t.Fatalf("Timeout waiting for Service to start")
	}

	// create the stub, should work if previous test passed
	stub := &SimpleInterface{}
	err = StubFactory(stub, addr, false, false)
	if err != nil {
		t.Fatalf("StubFactory failed to create service proxy from interface")
	}

	// Attempt to get a value from the stub.
	i, e, r := stub.Method(1, false)
	if i != 1 || e != "" || r.Error() != "" {
		fmt.Printf("i is %d, e is %s, r is %s\n", i, e, r.Error())
		t.Fatalf("Stub returned an incorrect result for argument false")
	}

	// Attempt to get an error message.
	i, e, r = stub.Method(1, true)
	if i != -1 || e == "" || r.Error() != "" {
		t.Fatalf("Stub returned an incorrect result for argument true")
	}
}

// TestFinal_LossyConnection -- tests connection between stub and service,
// when the connection is not reliable, including large number of remote calls.
func TestFinal_LossyConnection(t *testing.T) {

	// choose a large-ish random port number for each test
	port := rand.Intn(10000) + 7000
	addr := "127.0.0.1:" + strconv.Itoa(port)

	// create the service, should work if the previous test passed
	srvc, err := NewService(&SimpleInterface{}, &SimpleObject{}, port, true, true)
	if err != nil {
		t.Fatalf("Error in NewService: %s", err.Error())
	}
	if srvc == nil {
		t.Fatalf("NewService returned nil service")
	}

	err = srvc.Start()
	if err != nil {
		t.Fatalf("Error in Service.start(): %s", err.Error())
	}
	defer srvc.Stop()

	// wait for service to start...or timeout
	ddln := time.Now().Add(5 * time.Second)
	for !srvc.IsRunning() && time.Now().Before(ddln) {
	}
	if !srvc.IsRunning() {
		t.Fatalf("Timeout waiting for Service to start")
	}

	// create the stub, should work if previous test passed
	stub := &SimpleInterface{}
	err = StubFactory(stub, addr, true, true)
	if err != nil {
		t.Fatalf("StubFactory failed to create service proxy from interface")
	}

	// Attempt to get a whole lot of values from the stub.
	var i int
	var e string
	var r RemoteObjectError

	for j := 0; j < 100; j++ {
		i, e, r = stub.Method(j, false)
		if i != j || e != "" || r.Error() != "" {
			t.Fatalf("Stub returned an incorrect result for argument false")
		}
	}
}

// TestFinal_Multithread -- checks that the service supports multiple simultaneous
// connections, using the Rendezvous method in the SimpleInterface.
func TestFinal_Multithread(t *testing.T) {

	// choose a large-ish random port number for each test
	port := rand.Intn(10000) + 7000
	addr := "127.0.0.1:" + strconv.Itoa(port)

	// create the service, should work if the previous tests pass
	srvc, err := NewService(&SimpleInterface{}, &SimpleObject{}, port, false, false)
	if err != nil {
		t.Fatalf("Error in NewService: %s", err.Error())
	}
	if srvc == nil {
		t.Fatalf("NewService returned nil service")
	}

	err = srvc.Start()
	if err != nil {
		t.Fatalf("Error in Service.start(): %s", err.Error())
	}
	defer srvc.Stop()

	// wait for service to start...or timeout
	ddln := time.Now().Add(5 * time.Second)
	for !srvc.IsRunning() && time.Now().Before(ddln) {
	}
	if !srvc.IsRunning() {
		t.Fatalf("Timeout waiting for Service to start")
	}

	// create the stub, should work if previous tests pass
	stub := &SimpleInterface{}
	err = StubFactory(stub, addr, false, false)
	if err != nil {
		t.Fatalf("StubFactory failed to create service proxy from interface")
	}

	// create a second thread that calls rendezvous on the test object.
	go func() {
		// call the Rendezvous method
		r := stub.Rendezvous()
		if r.Error() != "" {
			t.Fatalf("Rendezvous call in go routine failed: %s", r.Error())
		}
	}()

	r := stub.Rendezvous()
	if r.Error() != "" {
		t.Fatalf("Rendezvous call failed: %s", r.Error())
	}
}

// TestFinal_Mismatch -- checks that the service handles incorrect method signatures,
// including extra methods, different argument number and types, and different return type
func TestFinal_Mismatch(t *testing.T) {

	// choose a large-ish random port number for each test
	port := rand.Intn(10000) + 7000
	addr := "127.0.0.1:" + strconv.Itoa(port)

	// create the service, should work if the previous tests pass
	srvc, err := NewService(&SimpleInterface{}, &SimpleObject{}, port, false, false)
	if err != nil {
		t.Fatalf("Error in NewService: %s", err.Error())
	}
	if srvc == nil {
		t.Fatalf("NewService returned nil service")
	}

	err = srvc.Start()
	if err != nil {
		t.Fatalf("Error in Service.start(): %s", err.Error())
	}
	defer srvc.Stop()

	// wait for service to start...or timeout
	ddln := time.Now().Add(5 * time.Second)
	for !srvc.IsRunning() && time.Now().Before(ddln) {
	}
	if !srvc.IsRunning() {
		t.Fatalf("Timeout waiting for Service to start")
	}

	// create the stub, should work if previous tests pass
	stub := &MismatchInterface{}
	err = StubFactory(stub, addr, false, false)
	if err != nil {
		t.Fatalf("StubFactory failed to create service proxy from MismatchInterface")
	}

	var r RemoteObjectError

	// invoke a method not in the SimpleInterface.
	r = stub.ExtraMethod()
	if r.Error() == "" {
		t.Fatalf("Service accepted undefined method from Stub")
	}

	// invoke a method with different argument types from SimpleInterface.
	_, _, r = stub.Method(1, 1)
	if r.Error() == "" {
		t.Fatalf("Service accepted incorrect argument type from Stub")
	}

	// second thread calls Rendezvous expecting two return values.
	go func() {
		// call the Rendezvous method
		_, r := stub.Rendezvous()
		if r.Error() == "" {
			t.Fatalf("Second thread accepted reply with wrong return arguments")
		}
	}()

	_, r = stub.Rendezvous()
	if r.Error() == "" {
		t.Fatalf("Stub accepted reply with wrong return arguments")
	}

	// create another stub, should work if previous tests pass
	stub2 := &MismatchInterface2{}
	err = StubFactory(stub2, addr, false, false)
	if err != nil {
		t.Fatalf("StubFactory failed to create service proxy from MismatchInterface2")
	}

	// invoke a method with more arguments than SimpleInterface.
	_, _, r = stub2.Method(1, true, 1)
	if r.Error() == "" {
		t.Fatalf("Service accepted incorrect argument type from Stub")
	}
}

// TestFinal_Reconnection -- tests connection and use, disconnection,
// reconnection and use, another disconnection, another reconnection and use.
func TestFinal_Reconnection(t *testing.T) {

	// choose a large-ish random port number for each test
	port := rand.Intn(10000) + 7000
	addr := "127.0.0.1:" + strconv.Itoa(port)

	// create the service, should work if the previous tests passed
	srvc, err := NewService(&SimpleInterface{}, &SimpleObject{}, port, false, false)
	if err != nil {
		t.Fatalf("Error in NewService: %s", err.Error())
	}
	if srvc == nil {
		t.Fatalf("NewService returned nil service")
	}

	// create the stub, should work if previous tests passed
	stub := &SimpleInterface{}
	err = StubFactory(stub, addr, false, false)
	if err != nil {
		t.Fatalf("StubFactory failed to create service proxy from interface")
	}

	err = srvc.Start()
	if err != nil {
		t.Fatalf("Error in Service.start(): %s", err.Error())
	}

	// wait for service to start...or timeout
	ddln := time.Now().Add(time.Second)
	for !srvc.IsRunning() && time.Now().Before(ddln) {
	}
	if !srvc.IsRunning() {
		t.Fatalf("Timeout waiting for Service to start")
	}

	// Attempt to get a value from the stub.
	i, e, r := stub.Method(1, false)
	if i != 1 || e != "" || r.Error() != "" {
		t.Fatalf("Stub returned an incorrect result for argument false")
	}

	srvc.Stop()

	// make a call that should fail because the service stopped.
	i, e, r = stub.Method(1, false)
	if r.Error() == "" {
		t.Fatalf("Call to stopped service didn't return an error")
	}

	time.Sleep(time.Second) // take a little nap then restart service
	err = srvc.Start()
	if err != nil {
		t.Fatalf("Error in Service.start() 2nd time: %s", err.Error())
	}

	// wait for service to start...or timeout
	ddln = time.Now().Add(time.Second)
	for !srvc.IsRunning() && time.Now().Before(ddln) {
	}
	if !srvc.IsRunning() {
		t.Fatalf("Timeout waiting for Service to start 2nd time")
	}

	// Attempt to get a value from the stub.
	i, e, r = stub.Method(1, false)
	if i != 1 || e != "" || r.Error() != "" {
		t.Fatalf("Stub returned an incorrect result for argument false")
	}

	srvc.Stop()

	// make a call that should fail because the service stopped.
	i, e, r = stub.Method(1, false)
	if r.Error() == "" {
		t.Fatalf("Call to stopped service didn't return an error")
	}

	time.Sleep(time.Second) // take a little nap then restart service
	err = srvc.Start()
	if err != nil {
		t.Fatalf("Error in Service.start() 3rd time: %s", err.Error())
	}

	// wait for service to start...or timeout
	ddln = time.Now().Add(time.Second)
	for !srvc.IsRunning() && time.Now().Before(ddln) {
	}
	if !srvc.IsRunning() {
		t.Fatalf("Timeout waiting for Service to start 3rd time")
	}

	// Attempt to get a value from the stub.
	i, e, r = stub.Method(1, false)
	if i != 1 || e != "" || r.Error() != "" {
		t.Fatalf("Stub returned an incorrect result for argument false")
	}

	srvc.Stop()
}
