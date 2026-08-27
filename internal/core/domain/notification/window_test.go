package notification_test

import (
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/notification"
)

var clock = time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)

func TestHowFarBackEachWindowReaches(t *testing.T) {
	const keeps = 30 * 24 * time.Hour

	cases := map[notification.Window]time.Duration{
		notification.WindowLastHour:  time.Hour,
		notification.WindowLastDay:   24 * time.Hour,
		notification.WindowLastWeek:  7 * 24 * time.Hour,
		notification.WindowLastMonth: 30 * 24 * time.Hour,
	}
	for w, want := range cases {
		t.Run(w.String(), func(t *testing.T) {
			if got := w.Length(keeps); got != want {
				t.Errorf("Length() = %v, want %v", got, want)
			}
			if got := w.Since(clock, keeps); !got.Equal(clock.Add(-want)) {
				t.Errorf("Since() = %v, want %v", got, clock.Add(-want))
			}
		})
	}
}

// The only value that is right whatever the retention age is set to, because it
// names no number of its own.
func TestTheWidestWindowIsWhateverIsKept(t *testing.T) {
	for _, keeps := range []time.Duration{time.Hour, 7 * 24 * time.Hour, 365 * 24 * time.Hour} {
		if got := notification.WindowAll.Length(keeps); got != keeps {
			t.Errorf("Length(%v) = %v, want the retention age itself", keeps, got)
		}
		if got := notification.WindowAll.Since(clock, keeps); !got.Equal(clock.Add(-keeps)) {
			t.Errorf("Since(%v) = %v, want %v", keeps, got, clock.Add(-keeps))
		}
	}
}

// It is the zero value on purpose: a caller that says nothing gets the widest
// honest answer rather than an empty one.
func TestTheZeroWindowIsTheWidest(t *testing.T) {
	var w notification.Window
	if w != notification.WindowAll {
		t.Errorf("zero window = %v, want WindowAll", w)
	}
}

func TestWhatIsNotAWindow(t *testing.T) {
	for _, w := range []notification.Window{
		notification.WindowAll,
		notification.WindowLastHour,
		notification.WindowLastDay,
		notification.WindowLastWeek,
		notification.WindowLastMonth,
	} {
		if !w.Valid() {
			t.Errorf("%v is a constant but Valid() says otherwise", w)
		}
	}

	for _, w := range []notification.Window{-1, 7, 99} {
		if w.Valid() {
			t.Errorf("Window(%d) passed Valid()", int8(w))
		}
	}
}

// An out-of-range value shows up in a log as Window(7) rather than vanishing.
func TestAnUnknownWindowIsStillReadable(t *testing.T) {
	if got := notification.Window(7).String(); got != "Window(7)" {
		t.Errorf("String() = %q, want a debuggable form", got)
	}
}
