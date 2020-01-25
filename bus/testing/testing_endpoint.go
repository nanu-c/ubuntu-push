/*
 Copyright 2013-2014 Canonical Ltd.

 This program is free software: you can redistribute it and/or modify it
 under the terms of the GNU General Public License version 3, as published
 by the Free Software Foundation.

 This program is distributed in the hope that it will be useful, but
 WITHOUT ANY WARRANTY; without even the implied warranties of
 MERCHANTABILITY, SATISFACTORY QUALITY, or FITNESS FOR A PARTICULAR
 PURPOSE.  See the GNU General Public License for more details.

 You should have received a copy of the GNU General Public License along
 with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package testing

// Here, the bus.Endpoint implementation.

import (
	"errors"
	"fmt"

	"launchpad.net/go-dbus"

	"github.com/ubports/ubuntu-push/bus"
	"github.com/ubports/ubuntu-push/testing/condition"
	"sync"
	"time"
)

type callArgs struct {
	Member string
	Args   []interface{}
}

type testingEndpoint struct {
	dialCond     condition.Interface
	callCond     condition.Interface
	usedLck      sync.Mutex
	used         int
	retvals      [][]interface{}
	watchSources map[string]chan []interface{}
	watchLck     sync.RWMutex
	callArgs     []callArgs
	callArgsLck  sync.RWMutex
}

// Build a bus.Endpoint that calls OK() on its condition before returning
// the provided return values.
//
// NOTE: Call() always returns the first return value; Watch() will provide
// each of them in turn, irrespective of whether Call has been called.
func NewMultiValuedTestingEndpoint(dialCond condition.Interface, callCond condition.Interface, retvalses ...[]interface{}) bus.Endpoint {
	return &testingEndpoint{dialCond: dialCond, callCond: callCond, retvals: retvalses, watchSources: make(map[string]chan []interface{})}
}

func NewTestingEndpoint(dialCond condition.Interface, callCond condition.Interface, retvals ...interface{}) bus.Endpoint {
	retvalses := make([][]interface{}, len(retvals))
	for i, x := range retvals {
		retvalses[i] = []interface{}{x}
	}
	return &testingEndpoint{dialCond: dialCond, callCond: callCond, retvals: retvalses, watchSources: make(map[string]chan []interface{})}
}

// If SetWatchSource is called with a non-nil watchSource, it is used
// instead of the default timeout and retvals to get values to send
// over WatchSignal. Set it to nil again to restore default behaviour.
func SetWatchSource(tc bus.Endpoint, member string, watchSource chan []interface{}) {
	tc.(*testingEndpoint).watchLck.Lock()
	tc.(*testingEndpoint).watchSources[member] = watchSource
	tc.(*testingEndpoint).watchLck.Unlock()
}

// GetCallArgs returns a list of the arguments for each Call() invocation.
func GetCallArgs(tc bus.Endpoint) []callArgs {
	tc.(*testingEndpoint).callArgsLck.RLock()
	defer tc.(*testingEndpoint).callArgsLck.RUnlock()
	return tc.(*testingEndpoint).callArgs
}

type watchCancel struct {
	done      chan struct{}
	cancelled chan struct{}
	lck       sync.Mutex
	member    string
}

// this waits for actual cancelllation for test convenience
func (wc *watchCancel) Cancel() error {
	wc.lck.Lock()
	defer wc.lck.Unlock()
	if wc.cancelled != nil {
		close(wc.cancelled)
		wc.cancelled = nil
		<-wc.done
	}
	return nil
}

// See Endpoint's WatchSignal. This WatchSignal will check its condition to
// decide whether to return an error, or provide each of its return values
// or values from the previously set watchSource for member.
func (tc *testingEndpoint) WatchSignal(member string, f func(...interface{}), d func()) (bus.Cancellable, error) {
	if tc.callCond.OK() {
		cancelled := make(chan struct{})
		done := make(chan struct{})
		go func() {
			tc.watchLck.RLock()
			source := tc.watchSources[member]
			tc.watchLck.RUnlock()
			if source == nil {
				tc.usedLck.Lock()
				idx := tc.used
				tc.used++
				tc.usedLck.Unlock()
				source = make(chan []interface{})
				go func() {
				Feed:
					for _, v := range tc.retvals[idx:] {
						select {
						case source <- v:
						case <-cancelled:
							break Feed
						}
						select {
						case <-time.After(10 * time.Millisecond):
						case <-cancelled:
							break Feed
						}
					}
					close(source)
				}()
			}
		Receive:
			for {
				select {
				case v, ok := <-source:
					if !ok {
						break Receive
					}
					f(v...)
				case <-cancelled:
					break Receive
				}
			}
			d()
			close(done)
		}()
		return &watchCancel{cancelled: cancelled, done: done, member: member}, nil
	} else {
		return nil, errors.New("no way")
	}
}

// See Endpoint's WatchProperties.
func (tc *testingEndpoint) WatchProperties(f func(map[string]dbus.Variant, []string), d func()) (bus.Cancellable, error) {
	translate := func(vals ...interface{}) {
		changed := vals[0].(map[string]dbus.Variant)
		invalidated := vals[1].([]string)
		f(changed, invalidated)
	}
	return tc.WatchSignal("PropertiesChanged", translate, d)
}

// See Endpoint's Call. This Call will check its condition to decide whether
// to return an error, or the first of its return values
func (tc *testingEndpoint) Call(member string, args []interface{}, rvs ...interface{}) error {
	tc.callArgsLck.Lock()
	defer tc.callArgsLck.Unlock()

	tc.callArgs = append(tc.callArgs, callArgs{member, args})
	if tc.callCond.OK() {
		expected := len(rvs)
		var provided int
		tc.usedLck.Lock()
		idx := tc.used
		tc.used++
		tc.usedLck.Unlock()
		if len(tc.retvals) <= idx {
			if expected != 0 {
				panic("No return values provided!")
			}
			provided = 0
		} else {
			provided = len(tc.retvals[idx])
		}
		if provided != expected {
			return errors.New("provided/expected return vals mismatch")
		}
		if provided != 0 {
			x := dbus.NewMethodCallMessage("", "", "", "")
			err := x.AppendArgs(tc.retvals[idx]...)
			if err != nil {
				return err
			}
			err = x.Args(rvs...)
			if err != nil {
				return err
			}
		}
		return nil
	} else {
		return errors.New("no way")
	}
}

// See Endpoint's GetProperty. This one is just another name for Call.
func (tc *testingEndpoint) GetProperty(property string) (interface{}, error) {
	var res interface{}
	err := tc.Call(property, bus.Args(), &res)
	if err != nil {
		return nil, err
	}
	return res, err
}

// See Endpoint's SetProperty. This one does nothing beyond
// registering being called.
func (tc *testingEndpoint) SetProperty(property string, suffix string, value interface{}) error {
	tc.callArgsLck.Lock()
	defer tc.callArgsLck.Unlock()

	args := callArgs{
		Member: "::SetProperty",
		Args:   []interface{}{property, suffix, value},
	}
	tc.callArgs = append(tc.callArgs, args)

	return nil
}

// See Endpoint's Dial. This one will check its dialCondition to
// decide whether to return an error or not.
func (endp *testingEndpoint) Dial() error {
	if endp.dialCond.OK() {
		return nil
	} else {
		return errors.New("dialCond said No.")
	}
}

// Advanced stringifobabulation
func (endp *testingEndpoint) String() string {
	return fmt.Sprintf("&testingEndpoint{dialCond:(%s) callCond:(%s) retvals:(%#v)",
		endp.dialCond, endp.callCond, endp.retvals)
}

// see Endpoint's Close. This one does nothing beyond registering
// being called.
func (tc *testingEndpoint) Close() {
	tc.callArgsLck.Lock()
	defer tc.callArgsLck.Unlock()

	args := callArgs{Member: "::Close"}
	tc.callArgs = append(tc.callArgs, args)
}

func (tc *testingEndpoint) GrabName(allowReplacement bool) <-chan error {
	tc.callArgsLck.Lock()
	defer tc.callArgsLck.Unlock()

	args := callArgs{Member: "::GrabName"}
	args.Args = append(args.Args, allowReplacement)
	tc.callArgs = append(tc.callArgs, args)

	return nil
}

func (tc *testingEndpoint) WatchMethod(dispatch bus.DispatchMap, suffix string, extra ...interface{}) {
	tc.callArgsLck.Lock()
	defer tc.callArgsLck.Unlock()

	args := callArgs{Member: "::WatchMethod"}
	args.Args = append(args.Args, dispatch, extra)
	tc.callArgs = append(tc.callArgs, args)
}

func (tc *testingEndpoint) Signal(member string, suffix string, args []interface{}) error {
	tc.callArgsLck.Lock()
	defer tc.callArgsLck.Unlock()

	callargs := callArgs{Member: "::Signal"}
	callargs.Args = append(callargs.Args, member, suffix, args)
	tc.callArgs = append(tc.callArgs, callargs)

	return nil
}

// ensure testingEndpoint implements bus.Endpoint
var _ bus.Endpoint = (*testingEndpoint)(nil)
