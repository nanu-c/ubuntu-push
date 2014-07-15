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

// package cmessaging wraps libmessaging-menu
package cmessaging

/*
#cgo pkg-config: messaging-menu

#include <glib.h>

void add_notification(const gchar* desktop_id, const gchar* notification_id,
          const gchar* icon_path, const gchar* summary, const gchar* body,
          gint64 timestamp, gpointer obj);
*/
import "C"
import "unsafe"

import (
	"launchpad.net/ubuntu-push/launch_helper"
	"launchpad.net/ubuntu-push/messaging/reply"
)

type Payload struct {
	Ch      chan *reply.MMActionReply
	Actions []string
}

func gchar(s string) *C.gchar {
	return (*C.gchar)(C.CString(s))
}

func gfree(s *C.gchar) {
	C.g_free((C.gpointer)(s))
}

//export handleActivate
func handleActivate(action *C.char, notification *C.char, obj unsafe.Pointer) {
	payload := (*Payload)(obj)
	mmar := &reply.MMActionReply{Notification: C.GoString(notification), Action: C.GoString(action)}
	payload.Ch <- mmar
}

func AddNotification(desktopId string, notificationId string, card *launch_helper.Card, payload *Payload) {
	desktop_id := gchar(desktopId)
	defer gfree(desktop_id)

	notification_id := gchar(notificationId)
	defer gfree(notification_id)

	icon_path := gchar(card.Icon)
	defer gfree(icon_path)

	summary := gchar(card.Summary)
	defer gfree(summary)

	body := gchar(card.Body)
	defer gfree(body)

	timestamp := (C.gint64)(int64(card.Timestamp) * 1000000)

	C.add_notification(desktop_id, notification_id, icon_path, summary, body, timestamp, (C.gpointer)(&payload))
}

func init() {
	go C.g_main_loop_run(C.g_main_loop_new(nil, C.FALSE))
}
