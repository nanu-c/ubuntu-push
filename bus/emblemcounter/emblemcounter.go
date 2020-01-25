/*
 Copyright 2014 Canonical Ltd.

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

// Package emblemcounter can present notifications as a counter on an
// emblem on an item in the launcher.
package emblemcounter

import (
	"launchpad.net/go-dbus"

	"github.com/ubports/ubuntu-push/bus"
	"github.com/ubports/ubuntu-push/click"
	"github.com/ubports/ubuntu-push/launch_helper"
	"github.com/ubports/ubuntu-push/logger"
	"github.com/ubports/ubuntu-push/nih"
)

// emblemcounter works by setting properties on a well-known dbus name.
var BusAddress = bus.Address{
	Interface: "com.canonical.Unity.Launcher.Item",
	Path:      "/com/canonical/Unity/Launcher",
	Name:      "com.canonical.Unity.Launcher",
}

// EmblemCounter is a little tool that fiddles with the unity launcher
// to put emblems with counters on launcher icons.
type EmblemCounter struct {
	bus bus.Endpoint
	log logger.Logger
}

// Build an EmblemCounter using the given bus and log.
func New(endp bus.Endpoint, log logger.Logger) *EmblemCounter {
	return &EmblemCounter{bus: endp, log: log}
}

// Look for an EmblemCounter section in a Notification and, if
// present, presents it to the user.
func (ctr *EmblemCounter) Present(app *click.AppId, nid string, notification *launch_helper.Notification) bool {
	if notification == nil {
		panic("please check notification is not nil before calling present")
	}

	ec := notification.EmblemCounter

	if ec == nil {
		ctr.log.Debugf("[%s] notification has no EmblemCounter: %#v", nid, ec)
		return false
	}
	ctr.log.Debugf("[%s] setting emblem counter for %s to %d (visible: %t)", nid, app.Base(), ec.Count, ec.Visible)
	return ctr.SetCounter(app, ec.Count, ec.Visible)
}

// SetCounter sets an emblem counter on the launcher for app to count (if
// visible is true), or clears it (if count is 0 or visible is false).
func (ctr *EmblemCounter) SetCounter(app *click.AppId, count int32, visible bool) bool {
	base := app.Base()
	quoted := string(nih.Quote([]byte(base)))

	err := ctr.bus.SetProperty("count", "/"+quoted, dbus.Variant{count})
	if err != nil {
		ctr.log.Errorf("call to set count failed: %v", err)
		return false
	}
	err = ctr.bus.SetProperty("countVisible", "/"+quoted, dbus.Variant{visible})
	if err != nil {
		ctr.log.Errorf("call to set countVisible failed: %v", err)
		return false
	}

	return true
}
