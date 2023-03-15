// support for generic Remote Object services over sockets
// including a socket wrapper that can drop and/or delay messages arbitrarily
// works with any* objects that can be gob-encoded for serialization
//
// the LeakySocket wrapper for net.Conn is provided in its entirety, and should
// not be changed, though you may extend it with additional helper functions as
// desired.  it is used directly by the test code.
//
// the RemoteObjectError type is also provided in its entirety, and should not
// be changed.
//
// suggested RequestMsg and ReplyMsg types are included to get you started,
// but they are only used internally to the remote library, so you can use
// something else if you prefer
//
// the Service type represents the callee that manages remote objects, invokes
// calls from callers, and returns suitable results and/or remote errors
//
// the StubFactory converts a struct of function declarations into a functional
// caller stub by automatically populating the function definitions.
//
// USAGE:
// the desired usage of this library is as follows (not showing all error-checking
// for clarity and brevity):
//
//  example ServiceInterface known to both client and server, defined as
//  type ServiceInterface struct {
//      ExampleMethod func(int, int) (int, remote.RemoteObjectError)
//  }
//
//  1. server-side program calls NewService with interface and connection details, e.g.,
//     obj := &ServiceObject{}
//     srvc, err := remote.NewService(&ServiceInterface{}, obj, 9999, true, true)
//
//  2. client-side program calls StubFactory, e.g.,
//     stub := &ServiceInterface{}
//     err := StubFactory(stub, 9999, true, true)
//
//  3. client makes calls, e.g.,
//     n, roe := stub.ExampleMethod(7, 14736)
//
//
//
//
//
// TODO *** here's what needs to be done for Lab 2:
//  1. create the Service type and supporting functions, including but not
//     limited to: NewService, Start, Stop, IsRunning, and GetCount (see below)
//
//  2. create the StubFactory which uses reflection to transparently define each
//     method call in the client-side stub (see below)
//

package remote

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"reflect"
	"strconv"

	// "strings"
	"sync"
	"time"
)

const (
	ServerAddress string = "localhost:"
	maxRetryTime  int    = 5
)

// LeakySocket
//
// LeakySocket is a wrapper for a net.Conn connection that emulates
// transmission delays and random packet loss. it has its own send
// and receive functions that together mimic an unreliable connection
// that can be customized to stress-test remote service interactions.
type LeakySocket struct {
	s         net.Conn
	isLossy   bool
	lossRate  float32
	msTimeout int
	usTimeout int
	isDelayed bool
	msDelay   int
	usDelay   int
}

// builder for a LeakySocket given a normal socket and indicators
// of whether the connection should experience loss and delay.
// uses default loss and delay values that can be changed using setters.
func NewLeakySocket(conn net.Conn, lossy bool, delayed bool) *LeakySocket {
	ls := &LeakySocket{}
	ls.s = conn
	ls.isLossy = lossy
	ls.isDelayed = delayed
	ls.msDelay = 2
	ls.usDelay = 0
	ls.msTimeout = 500
	ls.usTimeout = 0
	ls.lossRate = 0.05

	return ls
}

// send a byte-string over the socket mimicking unreliability.
// delay is emulated using time.Sleep, packet loss is emulated using RNG
// coupled with time.Sleep to emulate a timeout
func (ls *LeakySocket) SendObject(obj []byte) (bool, error) {
	if obj == nil {
		return true, nil
	}

	if ls.s != nil {
		rand.Seed(time.Now().UnixNano())
		if ls.isLossy && rand.Float32() < ls.lossRate {
			time.Sleep(time.Duration(ls.msTimeout)*time.Millisecond + time.Duration(ls.usTimeout)*time.Microsecond)
			return false, nil
		} else {
			if ls.isDelayed {
				time.Sleep(time.Duration(ls.msDelay)*time.Millisecond + time.Duration(ls.usDelay)*time.Microsecond)
			}
			_, err := ls.s.Write(obj)
			if err != nil {
				return false, errors.New("SendObject Write error: " + err.Error())
			}
			return true, nil
		}
	}
	return false, errors.New("SendObject failed, nil socket")
}

// receive a byte-string over the socket connection.
// no significant change to normal socket receive.
func (ls *LeakySocket) RecvObject() ([]byte, error) {
	if ls.s != nil {
		buf := make([]byte, 4096)
		n := 0
		var err error
		for n <= 0 {
			n, err = ls.s.Read(buf)
			if n > 0 {
				return buf[:n], nil
			}
			if err != nil {
				if err != io.EOF {
					return nil, errors.New("RecvObject Read error: " + err.Error())
				}
			}
		}
	}
	return nil, errors.New("RecvObject failed, nil socket")
}

// enable/disable emulated transmission delay and/or change the delay parameter
func (ls *LeakySocket) SetDelay(delayed bool, ms int, us int) {
	ls.isDelayed = delayed
	ls.msDelay = ms
	ls.usDelay = us
}

// change the emulated timeout period used with packet loss
func (ls *LeakySocket) SetTimeout(ms int, us int) {
	ls.msTimeout = ms
	ls.usTimeout = us
}

// enable/disable emulated packet loss and/or change the loss rate
func (ls *LeakySocket) SetLossRate(lossy bool, rate float32) {
	ls.isLossy = lossy
	ls.lossRate = rate
}

// close the socket (can also be done on original net.Conn passed to builder)
func (ls *LeakySocket) Close() error {
	return ls.s.Close()
}

// RemoteObjectError
//
// RemoteObjectError is a custom error type used for this library to identify remote methods.
// it is used by both caller and callee endpoints.
type RemoteObjectError struct {
	Err string
}

// getter for the error message included inside the custom error type
func (e *RemoteObjectError) Error() string { return e.Err }

// RequestMsg (this is only a suggestion, can be changed)
//
// RequestMsg represents the request message sent from caller to callee.
// it is used by both endpoints, and uses the reflect package to carry
// arbitrary argument types across the network.
type RequestMsg struct {
	Method string
	Args   []interface{}
}

// ReplyMsg (this is only a suggestion, can be changed)
//
// ReplyMsg represents the reply message sent from callee back to caller
// in response to a RequestMsg. it similarly uses reflection to carry
// arbitrary return types along with a success indicator to tell the caller
// whether the call was correctly handled by the callee. also includes
// a RemoteObjectError to specify details of any encountered failure.
type ReplyMsg struct {
	Success bool
	Reply   []interface{}
	Err     RemoteObjectError
}

// Service -- server side stub/skeleton
//
// A Service encapsulates a multithreaded TCP server that manages a single
// remote object on a single TCP port, which is a simplification to ease management
// of remote objects and interaction with callers.  Each Service is built
// around a single struct of function declarations. All remote calls are
// handled synchronously, meaning the lifetime of a connection is that of a
// sinngle method call.  A Service can encounter a number of different issues,
// and most of them will result in sending a failure response to the caller,
// including a RemoteObjectError with suitable details.
type Service struct {
	// TODO: populate with needed contents including, but not limited to:
	ifaceType  reflect.Type  //of the Service's interface (struct of Fields)
	ifaceValue reflect.Value // of the Service's interface
	objValue   reflect.Value // of the Service's remote object instance
	port       int
	lossy      bool
	delayed    bool
	runningMu  sync.Mutex
	running    bool
	ln         net.Listener
	count      int
	countMu    sync.Mutex
	//       - status and configuration parameters, as needed
}

// helper function to check if the provided service is remote
// complish by checking if they return a RemoteObjectError
func CheckRemote(ifc interface{}) bool {
	ifcValue := reflect.ValueOf(ifc)
	ifcType := ifcValue.Elem().Type()
	// iterate through all the fields (methods) in the interface
	for i := 0; i < ifcType.NumField(); i++ {
		field := ifcType.Field(i)
		if field.Type.Kind() == reflect.Func {
			funcType := field.Type
			numOut := funcType.NumOut()
			if numOut == 0 {
				return false
			}
			lastOut := funcType.Out(numOut - 1)
			if lastOut != reflect.TypeOf(RemoteObjectError{}) {
				return false
			}
		}
	}
	return true
}

// build a new Service instance around a given struct of supported functions,
// a local instance of a corresponding object that supports these functions,
// and arguments to support creation and use of LeakySocket-wrapped connections.
// performs the following:
// -- returns a local error if function struct or object is nil
// -- returns a local error if any function in the struct is not a remote function
// -- if neither error, creates and populates a Service and returns a pointer
func NewService(ifc interface{}, sobj interface{}, port int, lossy bool, delayed bool) (*Service, error) {

	// if ifc is a pointer to a struct with function declarations,
	// then reflect.TypeOf(ifc).Elem() is the reflected struct's Type

	// if sobj is a pointer to an object instance, then
	// reflect.ValueOf(sobj) is the reflected object's Value

	// TODO: get the Service ready to start
	if ifc == nil {
		return nil, errors.New("Service interface cannot be nil")
	}
	if sobj == nil {
		return nil, errors.New("Service instance cannot be nil")
	}
	ifcType := reflect.TypeOf(ifc)
	ifcValue := reflect.ValueOf(ifc)
	objValue := reflect.ValueOf(sobj)

	// Check if all functions in ifc are remote functions.
	// If any function is not a remote function, return an error.

	if !CheckRemote(ifc) {
		return nil, fmt.Errorf("%s is not a remote interface", ifcType.String())
	}

	s := &Service{
		ifaceType:  ifcType.Elem(),
		ifaceValue: ifcValue.Elem(),
		objValue:   objValue,
		port:       port,
		lossy:      lossy,
		delayed:    delayed,
		runningMu:  sync.Mutex{},
		running:    false,
		count:      0,
		countMu:    sync.Mutex{},
	}

	return s, nil
}

func (serv *Service) HandleClients() {
	for {
		serv.runningMu.Lock()
		running := serv.running
		serv.runningMu.Unlock()
		if !running {
			return
		}
		conn, err := serv.ln.Accept()
		if err != nil {
			return
		}
		go serv.Serve(conn)
	}
}

func (serv *Service) Serve(conn net.Conn) {
	gob.Register(RemoteObjectError{})

	// wrap the net.Conn with a LeakySocket
	ls := NewLeakySocket(conn, serv.lossy, serv.delayed)
	// 1. Receive a byte-string
	byteSlice, err := ls.RecvObject()
	if err != nil {
		return
	}
	// 2. Decode the byte-string
	// Decode the received byte string into a request object
	buf := bytes.NewBuffer(byteSlice)
	dec := gob.NewDecoder(buf)
	var req RequestMsg
	if err := dec.Decode(&req); err != nil {
		return
	}
	// Check if the request method exists on the service interface
	field, ok := serv.ifaceType.FieldByName(req.Method)
	errBool := false
	var errStr string
	if !ok {
		errBool = true
		errStr = "invalid or mismatched interface"
	}
	method := serv.objValue.MethodByName(field.Name)
	// Check the length of the request arguments
	if !errBool && len(req.Args) != method.Type().NumIn() {
		errBool = true
		errStr = "arguments length mismatched"
	}

	if !errBool {
		// Check the types of the request arguments
		for i, arg := range req.Args {
			argType := reflect.TypeOf(arg)
			expectedType := method.Type().In(i)
			if argType != expectedType {
				errBool = true
				errStr = fmt.Sprintf("argument %d type mismatched", i)
				break
			}
		}
	}

	// unpack the args and invoke the method
	var rply ReplyMsg
	if !errBool {
		args := make([]reflect.Value, len(req.Args))
		for i := range req.Args {
			args[i] = reflect.ValueOf(req.Args[i])
		}
		result := method.Call(args)
		// 5. Encode the reply message into a byte-string
		// get the interface slices from result reflect.value slices
		// iterate over the reflect.Value arguments and get their values
		resVals := make([]interface{}, 0, len(result))
		for _, r := range result {
			resVals = append(resVals, r.Interface())
		}
		rply = ReplyMsg{
			Success: true,
			Reply:   resVals,
			Err:     RemoteObjectError{},
		}
	} else {
		rply = ReplyMsg{
			Success: false,
			Reply:   make([]interface{}, 1),
			Err:     RemoteObjectError{Err: errStr},
		}
	}
	var buff bytes.Buffer
	enc := gob.NewEncoder(&buff)
	err = enc.Encode(&rply)
	if err != nil {
		fmt.Println(err)
	}
	// 6. Send the byte-string
	for {
		ok, _ = ls.SendObject(buff.Bytes())
		if ok {
			break
		}
	}
	serv.countMu.Lock()
	defer serv.countMu.Unlock()
	serv.count += 1
	conn.Close()
}

// start the Service's tcp listening connection, update the Service
// status, and start receiving caller connections
func (serv *Service) Start() error {
	// TODO: attempt to start a Service created using NewService
	//
	// if called on a service that is already running, print a warning
	// but don't return an error or do anything else
	if serv.running {
		fmt.Println("Service is already running")
		return nil
	}
	//
	// otherwise, start the multithreaded tcp server at the given address
	// and update Service state
	addr := ServerAddress + strconv.Itoa(serv.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Println(2)
		fmt.Println(err)
	}

	serv.ln = ln
	serv.running = true
	go serv.HandleClients()
	//
	// IMPORTANT: Start() should not be a blocking call. once the Service
	// is started, it should return
	//
	//
	// After the Service is started (not to be done inside of this Start
	//      function, but wherever you want):
	//
	// - accept new connections from client callers until someone calls
	//   Stop on this Service, spawning a thread to handle each one
	//
	// - within each client thread, wrap the net.Conn with a LeakySocket
	//   e.g., if Service accepts a client connection `c`, create a new
	//   LeakySocket ls as `ls := LeakySocket(c, ...)`.  then:
	//
	// 1. receive a byte-string on `ls` using `ls.RecvObject()`
	//
	// 2. decoding the byte-string
	//
	// 3. check to see if the service interface's Type includes a method
	//    with the given name
	//
	// 4. invoke method
	//
	// 5. encode the reply message into a byte-string
	//
	// 6. send the byte-string using `ls.SendObject`, noting that the configuration
	//    of the LossySocket does not guarantee that this will work...
	return nil
}

// get the total number of remote calls served successfully by this Service
func (serv *Service) GetCount() int {
	// TODO: return the total number of remote calls served successfully by this Service
	serv.countMu.Lock()
	defer serv.countMu.Unlock()
	return serv.count
}

// return a boolean value indicating whether the Service is running
func (serv *Service) IsRunning() bool {
	// TODO: return a boolean value indicating whether the Service is running
	return serv.running
}

// stop the Service, change state accordingly, clean up any resources
func (serv *Service) Stop() {
	// TODO: stop the Service, change state accordingly, clean up any resources
	serv.runningMu.Lock()
	serv.running = false
	serv.runningMu.Unlock()
	serv.ln.Close()
}

// StubFactory uses reflection to populate the interface functions to create the
// caller's stub interface. Only works if all functions are exported/public.
// Once created, the interface masks remote calls to a Service that hosts the
// object instance that the functions are invoked on.  The network address of the
// remote Service must be provided with the stub is created, and it may not change later.
// A call to StubFactory requires the following inputs:
// -- a struct of function declarations to act as the stub's interface/proxy
// -- the remote address of the Service as "<ip-address>:<port-number>"
// -- indicator of whether caller-to-callee channel has emulated packet loss
// -- indicator of whether caller-to-callee channel has emulated propagation delay
// performs the following:
// -- returns a local error if function struct is nil
// -- returns a local error if any function in the struct is not a remote function
// -- otherwise, uses relection to access the functions in the given struct and
//
//	populate their function definitions with the required stub functionality
func StubFactory(ifc interface{}, adr string, lossy bool, delayed bool) error {
	// if ifc is a pointer to a struct with function declarations,
	// then reflect.TypeOf(ifc).Elem() is the reflected struct's reflect.Type
	// and reflect.ValueOf(ifc).Elem() is the reflected struct's reflect.Value
	//
	// Here's what it needs to do (not strictly in this order):
	//
	//    1. create a request message populated with the method name and input
	//       arguments to send to the Service
	//
	//    2. create a []reflect.Value of correct size to hold the result to be
	//       returned back to the program
	//
	//    3. connect to the Service's tcp server, and wrap the connection in an
	//       appropriate LeakySocket using the parameters given to the StubFactory
	//
	//    4. encode the request message into a byte-string to send over the connection
	//
	//    5. send the encoded message, noting that the LeakySocket is not guaranteed
	//       to succeed depending on the given parameters
	//
	//    6. wait for a reply to be received using RecvObject, which is blocking
	//        -- if RecvObject returns an error, populate and return error output
	//
	//    7. decode the received byte-string according to the expected return types

	if ifc == nil {
		return errors.New("Service interface cannot be nil")
	}

	// check if ifc is remote
	if !CheckRemote(ifc) {
		return fmt.Errorf("ifc is not a remote interface")
	}

	// check if the service interface is closed

	// get the reflect.Type of the interface
	ifcType := reflect.TypeOf(ifc).Elem()

	// create a new value of the interface to act as the stub
	stubValue := reflect.New(ifcType)
	// iterate over the interface methods
	for i := 0; i < ifcType.NumField(); i++ {
		// get the method type and value
		methodType := ifcType.Field(i)
		methodValue := stubValue.Elem().FieldByName(methodType.Name)
		reqType := ifcType.Field(i).Type
		// wait for a reply message using the leaky socket
		methodType_type := reqType
		var reply ReplyMsg
		// make a new function called newFunc
		newFunc := func(args []reflect.Value) (results []reflect.Value) {
			gob.Register(RemoteObjectError{})
			// res checking
			hasError := false
			hassConn := true
			conn, err := net.Dial("tcp", adr)
			if err != nil {
				// fmt.Println(err)
				hasError = true
				hassConn = false
			}
			if !hasError {
				// wrap the connection in a leaky socket
				ls := NewLeakySocket(conn, lossy, delayed)

				// create a slice to hold the arguments
				argVals := make([]interface{}, 0, len(args))
				// iterate over the reflect.Value arguments and get their values
				for _, arg := range args {
					argVals = append(argVals, arg.Interface())
				}
				// create a request message with the method name and arguments
				reqMsg := RequestMsg{
					Method: methodType.Name,
					Args:   argVals,
				}
				// encode the message into a byte string
				var buf bytes.Buffer
				enc := gob.NewEncoder(&buf)
				err = enc.Encode(reqMsg)
				if err != nil {
					fmt.Println(err)
				}
				// send the message using the leaky socket
				for {
					ok, _ := ls.SendObject(buf.Bytes())
					if ok {
						break
					}
				}
				var repData []byte
				repData, err = ls.RecvObject()
				if err != nil {
					fmt.Println(err)
				}
				// decode the reply message into the expected return type
				decoder := gob.NewDecoder(bytes.NewReader(repData))
				err = decoder.Decode(&reply)
				if err != nil {
					fmt.Println(err)
				}

				var resVals []reflect.Value
				// append result from service to the returned
				for j := 0; j < len(reply.Reply); j++ {
					resVals = append(resVals, reflect.ValueOf(reply.Reply[j]))
				}
				// check if received a success message from the service
				if reply.Success {
					// check if the reply nums are the same
					if methodType_type.NumOut() != len(resVals) {
						reply.Err.Err = "result length does not match"
						hasError = true
					}

					// check if return types matched with expected outputs
					if !hasError {
						for j := 0; j < methodType_type.NumOut(); j++ {
							if methodType_type.Out(j) != resVals[j].Type() {
								reply.Err.Err = "result type does not match"
								hasError = false
								break
							}
						}
					}
					if !hasError {
						conn.Close()
						return resVals
					}
				}
			}

			ErrVals := []reflect.Value{}
			for k := 0; k < methodType_type.NumOut()-1; k++ {
				ErrVals = append(ErrVals, reflect.Zero(methodType_type.Out(k)))
			}
			if hassConn {
				conn.Close()
				ErrVals = append(ErrVals, reflect.ValueOf(reply.Err))
			}
			ErrVals = append(ErrVals, reflect.ValueOf(RemoteObjectError{Err: "Server stopped."}))
			return ErrVals
		}
		// create a new function value with the new function
		newFuncValue := reflect.MakeFunc(methodType.Type, newFunc)

		// set the new function value to the method value
		methodValue.Set(newFuncValue)
	}

	// set the stub value to the original interface value
	reflect.ValueOf(ifc).Elem().Set(stubValue.Elem())
	return nil
}
