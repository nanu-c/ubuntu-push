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

// Package notifications wraps a couple of Notifications's DBus API points:
// the org.freedesktop.Notifications.Notify call, and listening for the
// ActionInvoked signal.
package notifications

import (
	"encoding/json"
	"testing"
	"time"

	"launchpad.net/go-dbus"
	. "launchpad.net/gocheck"

	"github.com/ubports/ubuntu-push/bus"
	testibus "github.com/ubports/ubuntu-push/bus/testing"
	"github.com/ubports/ubuntu-push/click"
	clickhelp "github.com/ubports/ubuntu-push/click/testing"
	"github.com/ubports/ubuntu-push/launch_helper"
	helpers "github.com/ubports/ubuntu-push/testing"
	"github.com/ubports/ubuntu-push/testing/condition"
)

// hook up gocheck
func TestRaw(t *testing.T) { TestingT(t) }

type RawSuite struct {
	log *helpers.TestLogger
	app *click.AppId
	snd *mockSound
}

type mockSound struct{}

func (m *mockSound) Present(app *click.AppId, nid string, notification *launch_helper.Notification) bool {
	return false
}
func (m *mockSound) GetSound(app *click.AppId, nid string, notification *launch_helper.Notification) string {
	return "/usr/share/sounds/ubuntu/notifications/Xylo.ogg"
}

func (s *RawSuite) SetUpTest(c *C) {
	s.log = helpers.NewTestLogger(c, "debug")
	s.app = clickhelp.MustParseAppId("com.example.test_test-app_0")
	s.snd = &mockSound{}
}

var _ = Suite(&RawSuite{})

func (s *RawSuite) TestNotifies(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(true), uint32(1))
	raw := Raw(endp, s.log, nil)
	nid, err := raw.Notify("", 0, "", "", "", nil, nil, 0)
	c.Check(err, IsNil)
	c.Check(nid, Equals, uint32(1))
}

func (s *RawSuite) TestNotifiesFails(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(false))
	raw := Raw(endp, s.log, nil)
	_, err := raw.Notify("", 0, "", "", "", nil, nil, 0)
	c.Check(err, NotNil)
}

func (s *RawSuite) TestNotifyFailsIfNoBus(c *C) {
	raw := Raw(nil, s.log, nil)
	_, err := raw.Notify("", 0, "", "", "", nil, nil, 0)
	c.Check(err, ErrorMatches, `.*unconfigured .*`)
}

func (s *RawSuite) TestNotifiesFailsWeirdly(c *C) {
	endp := testibus.NewMultiValuedTestingEndpoint(nil, condition.Work(true), []interface{}{1, 2})
	raw := Raw(endp, s.log, nil)
	_, err := raw.Notify("", 0, "", "", "", nil, nil, 0)
	c.Check(err, NotNil)
}

func (s *RawSuite) TestWatchActions(c *C) {
	act := &RawAction{
		App:      clickhelp.MustParseAppId("_foo"),
		Nid:      "notif-id",
		ActionId: 1,
		Action:   "hello",
		RawId:    0,
	}
	jAct, err := json.Marshal(act)
	c.Assert(err, IsNil)
	endp := testibus.NewMultiValuedTestingEndpoint(nil, condition.Work(true),
		[]interface{}{uint32(1), string(jAct)})
	raw := Raw(endp, s.log, nil)
	ch, err := raw.WatchActions()
	c.Assert(err, IsNil)
	// check we get the right action reply
	act.RawId = 1 // checking the RawId is overwritten with the one in the ActionInvoked
	select {
	case p := <-ch:
		c.Check(p, DeepEquals, act)
	case <-time.NewTimer(time.Second / 10).C:
		c.Error("timed out?")
	}
	// and that the channel is closed if/when the watch fails
	_, ok := <-ch
	c.Check(ok, Equals, false)
}

type tst struct {
	errstr string
	endp   bus.Endpoint
	works  bool
	src    chan []interface{}
}

func (s *RawSuite) TestWatchActionsToleratesDBusWeirdness(c *C) {
	X := func(errstr string, args ...interface{}) tst {
		endp := testibus.NewMultiValuedTestingEndpoint(nil, condition.Work(true))
		src := make(chan []interface{}, 1)
		testibus.SetWatchSource(endp, "ActionInvoked", src)
		src <- args
		return tst{errstr, endp, errstr == "", src}
	}

	ts := []tst{
		X("delivered 0 things instead of 2"),
		X("delivered 1 things instead of 2", 2),
		X("1st param not a uint32", 1, "foo"),
		X("2nd param not a string", uint32(1), nil),
		X("2nd param not a json-encoded RawAction", uint32(1), ``),
		X("", uint32(1), `{}`),
	}

	for i, t := range ts {
		raw := Raw(t.endp, s.log, nil)
		ch, err := raw.WatchActions()
		c.Assert(err, IsNil)
		select {
		case p := <-ch:
			if !t.works {
				c.Errorf("got something on the channel! %#v (iter: %d)", p, i)
			}
		case <-time.After(time.Second / 10):
			if t.works {
				c.Errorf("failed to get something on the channel (iter: %d)", i)
			}
		}
		c.Check(s.log.Captured(), Matches, `(?ms).*`+t.errstr+`.*`)
		s.log.ResetCapture()
		close(t.src)
	}

}

func (s *RawSuite) TestWatchActionsFails(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(false))
	raw := Raw(endp, s.log, nil)
	_, err := raw.WatchActions()
	c.Check(err, NotNil)
}

func (s *RawSuite) TestPresentNotifies(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(true), uint32(1))
	raw := Raw(endp, s.log, nil)
	worked := raw.Present(s.app, "notifId", &launch_helper.Notification{Card: &launch_helper.Card{Summary: "summary", Popup: true}})
	c.Check(worked, Equals, true)
}

func (s *RawSuite) TestPresentWithSoundNotifies(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(true), uint32(1))
	raw := &RawNotifications{bus: endp, log: s.log, sound: s.snd}
	id := "notifId"
	notification := &launch_helper.Notification{
		Card: &launch_helper.Card{
			Summary: "summary", Popup: true,
		},
		RawSound: json.RawMessage(`true`),
	}
	worked := raw.Present(s.app, id, notification)
	sound := s.snd.GetSound(s.app, id, notification)
	c.Check(worked, Equals, true)
	c.Check(s.log.Captured(), Matches, `(?m).*notification will play sound: `+sound+`.*`)
}

func (s *RawSuite) TestPresentOneAction(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(true), uint32(1))
	raw := Raw(endp, s.log, nil)
	worked := raw.Present(s.app, "notifId", &launch_helper.Notification{Card: &launch_helper.Card{Summary: "summary", Popup: true, Actions: []string{"Yes"}}})
	c.Check(worked, Equals, true)
	callArgs := testibus.GetCallArgs(endp)
	c.Assert(callArgs, HasLen, 1)
	c.Assert(callArgs[0].Member, Equals, "Notify")
	c.Assert(len(callArgs[0].Args), Equals, 8)
	actions, ok := callArgs[0].Args[5].([]string)
	c.Assert(ok, Equals, true)
	c.Assert(actions, HasLen, 2)
	c.Check(actions[0], Equals, `{"app":"com.example.test_test-app_0","act":"Yes","nid":"notifId"}`)
	c.Check(actions[1], Equals, "Yes")
	hints, ok := callArgs[0].Args[6].(map[string]*dbus.Variant)
	c.Assert(ok, Equals, true)
	// with one action, there should be 2 hints set:
	c.Assert(hints, HasLen, 2)
	c.Check(hints["x-canonical-switch-to-application"], NotNil)
	c.Check(hints["x-canonical-secondary-icon"], NotNil)
	c.Check(hints["x-canonical-snap-decisions"], IsNil)
	c.Check(hints["x-canonical-private-button-tint"], IsNil)
}

func (s *RawSuite) TestPresentTwoActions(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(true), uint32(1))
	raw := Raw(endp, s.log, nil)
	worked := raw.Present(s.app, "notifId", &launch_helper.Notification{Card: &launch_helper.Card{Summary: "summary", Popup: true, Actions: []string{"Yes", "No"}}})
	c.Check(worked, Equals, true)
	callArgs := testibus.GetCallArgs(endp)
	c.Assert(callArgs, HasLen, 1)
	c.Assert(callArgs[0].Member, Equals, "Notify")
	c.Assert(len(callArgs[0].Args), Equals, 8)
	actions, ok := callArgs[0].Args[5].([]string)
	c.Assert(ok, Equals, true)
	c.Assert(actions, HasLen, 2)
	c.Check(actions[0], Equals, `{"app":"com.example.test_test-app_0","act":"Yes","nid":"notifId"}`)
	c.Check(actions[1], Equals, "Yes")
	// note that the rest are ignored.
	hints, ok := callArgs[0].Args[6].(map[string]*dbus.Variant)
	c.Assert(ok, Equals, true)
	c.Assert(hints, HasLen, 2)
	c.Check(hints["x-canonical-switch-to-application"], NotNil)
	c.Check(hints["x-canonical-secondary-icon"], NotNil)
}

func (s *RawSuite) TestPresentUsesSymbolic(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(true), uint32(1))
	raw := Raw(endp, s.log, nil)
	worked := raw.Present(s.app, "notifId", &launch_helper.Notification{Card: &launch_helper.Card{Summary: "summary", Popup: true}})
	c.Assert(worked, Equals, true)
	callArgs := testibus.GetCallArgs(endp)
	c.Assert(callArgs, HasLen, 1)
	c.Assert(callArgs[0].Args, HasLen, 8)
	hints, ok := callArgs[0].Args[6].(map[string]*dbus.Variant)
	c.Assert(ok, Equals, true)
	c.Check(hints["x-canonical-secondary-icon"].Value.(string), Equals, "-symbolic")
}

func (s *RawSuite) TestPresentNoNotificationPanics(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(true), uint32(1))
	raw := Raw(endp, s.log, nil)
	c.Check(func() { raw.Present(s.app, "notifId", nil) }, Panics, `please check notification is not nil before calling present`)
}

func (s *RawSuite) TestPresentNoCardDoesNotNotify(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(true), uint32(1))
	raw := Raw(endp, s.log, nil)
	worked := raw.Present(s.app, "notifId", &launch_helper.Notification{})
	c.Check(worked, Equals, false)
}

func (s *RawSuite) TestPresentNoSummaryDoesNotNotify(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(true), uint32(1))
	raw := Raw(endp, s.log, nil)
	worked := raw.Present(s.app, "notifId", &launch_helper.Notification{Card: &launch_helper.Card{}})
	c.Check(worked, Equals, false)
}

func (s *RawSuite) TestPresentNoPopupNoNotify(c *C) {
	endp := testibus.NewTestingEndpoint(nil, condition.Work(true), uint32(1))
	raw := Raw(endp, s.log, nil)
	worked := raw.Present(s.app, "notifId", &launch_helper.Notification{Card: &launch_helper.Card{Summary: "summary"}})
	c.Check(worked, Equals, false)
}
