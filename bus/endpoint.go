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

package bus

// Here we define the Endpoint, which represents the DBus connection itself.

import (
	"encoding/base64"
	"errors"
	"fmt"

	"launchpad.net/go-dbus"

	"github.com/ubports/ubuntu-push/logger"
)

/*****************************************************************
 *    Endpoint (and its implementation)
 */

type BusMethod func(string, []interface{}, []interface{}) ([]interface{}, error)
type DispatchMap map[string]BusMethod

// Cancellable can be canceled.
type Cancellable interface {
	Cancel() error
}

// bus.Endpoint represents the DBus connection itself.
type Endpoint interface {
	GrabName(allowReplacement bool) <-chan error
	WatchSignal(member string, f func(...interface{}), d func()) (Cancellable, error)
	WatchMethod(DispatchMap, string, ...interface{})
	Signal(string, string, []interface{}) error
	Call(member string, args []interface{}, rvs ...interface{}) error
	GetProperty(property string) (interface{}, error)
	SetProperty(property string, suffix string, value interface{}) error
	WatchProperties(f func(map[string]dbus.Variant, []string), d func()) (Cancellable, error)
	Dial() error
	Close()
	String() string
}

type endpoint struct {
	busT  Bus
	bus   *dbus.Connection
	proxy *dbus.ObjectProxy
	addr  Address
	log   logger.Logger
}

// constructor
func newEndpoint(bus Bus, addr Address, log logger.Logger) *endpoint {
	return &endpoint{busT: bus, addr: addr, log: log}
}

// ensure endpoint implements Endpoint
var _ Endpoint = (*endpoint)(nil)

/*
   public methods

XXX:   these are almost entirely untested, as that would need
XXX:   integration tests we are currently missing.
*/

// Dial() (re)establishes the connection with dbus
//
// XXX: mostly untested
func (endp *endpoint) Dial() error {
	bus, err := dbus.Connect(endp.busT.(concreteBus).dbusType())
	if err != nil {
		return err
	}
	d := dbus.BusDaemon{bus.Object(dbus.BUS_DAEMON_NAME, dbus.BUS_DAEMON_PATH)}
	name := endp.addr.Name
	hasOwner, err := d.NameHasOwner(name)
	if err != nil {
		endp.log.Debugf("unable to determine ownership of %#v: %v", name, err)
		bus.Close()
		return err
	}
	if !hasOwner {
		// maybe it's waiting to be activated?
		names, err := d.ListActivatableNames()
		if err != nil {
			endp.log.Debugf("%#v has no owner, and when listing activatable: %v", name, err)
			bus.Close()
			return err
		}
		found := false
		for _, name := range names {
			if name == name {
				found = true
				break
			}
		}
		if !found {
			msg := fmt.Sprintf("%#v has no owner, and not in activatables", name)
			endp.log.Debugf(msg)
			bus.Close()
			return errors.New(msg)
		}
	}
	endp.log.Debugf("%#v dialed in.", name)
	endp.bus = bus
	endp.proxy = bus.Object(name, dbus.ObjectPath(endp.addr.Path))
	return nil
}

// WatchSignal() takes a member name, sets up a watch for it (on the name,
// path and interface provided when creating the endpoint), and then calls f()
// with the unpacked value. If it's unable to set up the watch it returns an
// error. If the watch fails once established, d() is called. Typically f()
// sends the values over a channel, and d() would close the channel.
//
// XXX: untested
func (endp *endpoint) WatchSignal(member string, f func(...interface{}), d func()) (Cancellable, error) {
	watch, err := endp.proxy.WatchSignal(endp.addr.Interface, member)
	if err != nil {
		endp.log.Debugf("failed to set up the watch: %s", err)
		return nil, err
	}

	go endp.unpackMessages(watch, f, d, member)

	return watch, nil
}

// Call() invokes the provided member method (on the name, path and
// interface provided when creating the endpoint). args can be built
// using bus.Args(...). The return value is unpacked into rvs before being
// returned.
//
// XXX: untested
func (endp *endpoint) Call(member string, args []interface{}, rvs ...interface{}) error {
	msg, err := endp.proxy.Call(endp.addr.Interface, member, args...)
	if err != nil {
		return err
	}
	err = msg.Args(rvs...)
	if err != nil {
		return err
	}
	return nil
}

// GetProperty uses the org.freedesktop.DBus.Properties interface's Get method
// to read a given property on the name, path and interface provided when
// creating the endpoint. The return value is unpacked into a dbus.Variant,
// and its value returned.
//
// XXX: untested
func (endp *endpoint) GetProperty(property string) (interface{}, error) {
	msg, err := endp.proxy.Call("org.freedesktop.DBus.Properties", "Get", endp.addr.Interface, property)
	if err != nil {
		return nil, err
	}
	variantvs := endp.unpackOneMsg(msg, property)
	switch len(variantvs) {
	default:
		return nil, fmt.Errorf("too many values in Properties.Get response: %d", len(variantvs))
	case 0:
		return nil, fmt.Errorf("not enough values in Properties.Get response: %d", len(variantvs))
	case 1:
		// carry on
	}
	variant, ok := variantvs[0].(*dbus.Variant)
	if !ok {
		return nil, fmt.Errorf("response from Properties.Get wasn't a *dbus.Variant")
	}
	return variant.Value, nil
}

// SetProperty calls org.freedesktop.DBus.Properties's Set method
//
// XXX: untested
func (endp *endpoint) SetProperty(property string, suffix string, value interface{}) error {
	// can't use the pre-existing ObjectProxy for this one
	proxy := endp.bus.Object(endp.addr.Name, dbus.ObjectPath(endp.addr.Path+suffix))
	_, err := proxy.Call("org.freedesktop.DBus.Properties", "Set", endp.addr.Interface, property, value)
	return err
}

// WatchProperties() sets up a watch for
// org.freedesktop.DBus.Properties PropertiesChanged signal for the
// path and interface provided when creating the endpoint, and then
// calls f() with the unpacked value. If it's unable to set up the
// watch it returns an error. If the watch fails once established, d()
// is called. Typically f() sends the values over a channel, and d()
// would close the channel.
//
// XXX: untested
func (endp *endpoint) WatchProperties(f func(map[string]dbus.Variant, []string), d func()) (Cancellable, error) {
	watch, err := endp.proxy.WatchSignal("org.freedesktop.DBus.Properties", "PropertiesChanged")
	if err != nil {
		endp.log.Debugf("failed to set up the watch: %s", err)
		return nil, err
	}

	go func() {
		for {
			msg, ok := <-watch.C
			if !ok {
				break
			}
			var intfName string
			var changed map[string]dbus.Variant
			var invalidated []string
			if err := msg.Args(&intfName, &changed, &invalidated); err != nil {
				endp.log.Errorf("unexpected values from Properties watch")
				break
			}
			if intfName != endp.addr.Interface {
				// ignore
				continue
			}
			f(changed, invalidated)
		}
		endp.log.Debugf("got not-OK from Properties watch")
		d()
	}()

	return watch, nil
}

// Close the connection to dbus.
//
// XXX: untested
func (endp *endpoint) Close() {
	if endp.bus != nil {
		endp.bus.Close()
		endp.bus = nil
		endp.proxy = nil
	}
}

// String() performs advanced endpoint stringification
//
// XXX: untested
func (endp *endpoint) String() string {
	return fmt.Sprintf("<Connection to %s %#v>", endp.bus, endp.addr)
}

// GrabName() takes over the name on the bus, reporting errors over the
// returned channel.
//
// While the first result will be nil on success, successive results would
// typically indicate another process trying to take over the name.
//
// XXX: untested
func (endp *endpoint) GrabName(allowReplacement bool) <-chan error {
	flags := dbus.NameFlagAllowReplacement | dbus.NameFlagReplaceExisting
	if !allowReplacement {
		flags = 0
	}
	return endp.bus.RequestName(endp.addr.Name, flags).C
}

// Signal() sends out a signal called <member> containing <args>.
//
// XXX: untested
func (endp *endpoint) Signal(member string, suffix string, args []interface{}) error {
	path := dbus.ObjectPath(endp.addr.Path + suffix)
	msg := dbus.NewSignalMessage(path, endp.addr.Interface, member)
	if args != nil {
		err := msg.AppendArgs(args...)
		if err != nil {
			endp.log.Errorf("unable to build dbus signal message: %v", err)
			return err
		}
	}
	err := endp.bus.Send(msg)
	if err != nil {
		endp.log.Errorf("unable to send dbus signal: %v", err)
	} else {
		endp.log.Debugf("sent dbus signal %s(%#v)", member, args)
	}
	return nil
}

// WatchMethod() uses the given DispatchMap to answer incoming method
// calls.
//
// XXX: untested
func (endp *endpoint) WatchMethod(dispatch DispatchMap, suffix string, extra ...interface{}) {
	ch := make(chan *dbus.Message)
	go func() {
		var reply *dbus.Message

		err_iface := endp.addr.Interface + ".Error"

		for msg := range ch {
			meth, ok := dispatch[msg.Member]
			if !ok || msg.Interface != endp.addr.Interface {
				reply = dbus.NewErrorMessage(msg,
					"org.freedesktop.DBus.Error.UnknownMethod", "Unknown method")
				endp.log.Errorf("WatchMethod: unknown method %s", msg.Member)
			} else {
				args := msg.AllArgs()
				rvals, err := meth(string(msg.Path), args, extra)
				if err != nil {
					reply = dbus.NewErrorMessage(msg, err_iface, err.Error())
					endp.log.Errorf("WatchMethod: %s(%v, %#v, %#v) failure: %#v", msg.Member, msg.Path, args, extra, err)
				} else {
					var san_rvals []string
					for _, element := range rvals {
						sane := fmt.Sprintf("%v", element)
						_, err := base64.StdEncoding.DecodeString(sane)
						if err == nil {
							sane = "LooksLikeAToken=="
						}
						san_rvals = append(san_rvals, sane)
					}
					endp.log.Debugf("WatchMethod: %s(%v, %#v, %#v) success: %#v", msg.Member, msg.Path, args, extra, san_rvals)
					reply = dbus.NewMethodReturnMessage(msg)
					err = reply.AppendArgs(rvals...)
					if err != nil {
						endp.log.Errorf("WatchMethod: unable to build dbus response message: %v", err)
						reply = dbus.NewErrorMessage(msg, err_iface, err.Error())
					}
				}
			}
			err := endp.bus.Send(reply)
			if err != nil {
				endp.log.Errorf("WatchMethod: unable to send reply: %v", err)
			}

		}
	}()
	path := dbus.ObjectPath(endp.addr.Path + suffix)
	endp.bus.RegisterObjectPath(path, ch)
}

/*
   private methods
*/

// unpackOneMsg unpacks the value from the response msg
//
// XXX: untested
func (endp *endpoint) unpackOneMsg(msg *dbus.Message, member string) []interface{} {
	var varmap map[string]dbus.Variant
	if err := msg.Args(&varmap); err != nil {
		return msg.AllArgs()
	}
	return []interface{}{varmap}
}

// unpackMessages unpacks the value from the watch
//
// XXX: untested
func (endp *endpoint) unpackMessages(watch *dbus.SignalWatch, f func(...interface{}), d func(), member string) {
	for {
		msg, ok := <-watch.C
		if !ok {
			break
		}
		f(endp.unpackOneMsg(msg, member)...)
	}
	endp.log.Debugf("got not-OK from %s watch", member)
	d()
}
