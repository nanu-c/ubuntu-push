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

package util

import (
	"io/ioutil"
	. "launchpad.net/gocheck"
	testibus "launchpad.net/ubuntu-push/bus/testing"
	"launchpad.net/ubuntu-push/logger"
	"launchpad.net/ubuntu-push/testing/condition"
	"testing"
	"time"
)

// hook up gocheck
func TestRedialer(t *testing.T) { TestingT(t) }

type RedialerSuite struct {
	timeouts []time.Duration
}

var nullog = logger.NewSimpleLogger(ioutil.Discard, "error")
var _ = Suite(&RedialerSuite{})

func (s *RedialerSuite) SetUpSuite(c *C) {
	s.timeouts = Timeouts
	Timeouts = []time.Duration{0, 0}
}

func (s *RedialerSuite) TearDownSuite(c *C) {
	Timeouts = s.timeouts
	s.timeouts = nil
}

func (s *RedialerSuite) TestWorks(c *C) {
	endp := testibus.NewTestingEndpoint(condition.Fail2Work(3), nil)
	// instead of bus.Dial(), we do AutoRedial(bus)
	c.Check(AutoRedial(endp), Equals, uint32(4))
}

func (s *RedialerSuite) TestCanBeStopped(c *C) {
	endp := testibus.NewTestingEndpoint(condition.Work(false), nil)
	go func() { c.Check(AutoRedial(endp), Equals, uint32(1)) }()
	quitRedialing <- true
}

func (s *RedialerSuite) TestJitter(c *C) {
	num_tries := 20       // should do the math
	spread := time.Second //
	has_neg := false
	has_pos := false
	has_zero := true
	for i := 0; i < num_tries; i++ {
		n := Jitter(spread)
		if n > 0 {
			has_pos = true
		} else if n < 0 {
			has_neg = true
		} else {
			has_zero = true
		}
	}
	c.Check(has_neg, Equals, true)
	c.Check(has_pos, Equals, true)
	c.Check(has_zero, Equals, true)

	// a negative spread is caught in the reasonable place
	c.Check(func() { Jitter(time.Duration(-1)) }, PanicMatches,
		"spread must be non-negative")
}