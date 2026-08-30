package logincode_test

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/logincode"
	"github.com/Serajian/srosha/internal/core/shared"
)

var (
	codeID = shared.ID("01K0CDE00000000000000000AB")
	userID = shared.ID("01K0ACCT0000000000000000AB")
	now    = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
)

func TestTheRightCodeIsAccepted(t *testing.T) {
	c := logincode.New(codeID, userID, "123456", now)

	if err := c.Check("123456", now.Add(time.Minute)); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if c.UsedAt == nil {
		t.Error("a code that worked was not spent")
	}
}

// Spent by the FIRST attempt, right or wrong. Otherwise a wrong guess costs
// nothing and the guess limit is the only thing standing in the way.
func TestACodeIsSpentByOneAttempt(t *testing.T) {
	c := logincode.New(codeID, userID, "123456", now)

	if err := c.Check("000000", now); !errors.Is(err, logincode.ErrWrong) {
		t.Fatalf("first guess = %v, want ErrWrong", err)
	}

	// And the right code no longer works.
	if err := c.Check("123456", now); !errors.Is(err, logincode.ErrSpent) {
		t.Errorf("second attempt = %v, want ErrSpent", err)
	}
}

func TestAnExpiredCode(t *testing.T) {
	c := logincode.New(codeID, userID, "123456", now)

	err := c.Check("123456", now.Add(logincode.Lifetime+time.Second))
	if !errors.Is(err, logincode.ErrExpired) {
		t.Errorf("Check = %v, want ErrExpired", err)
	}
}

// A code already spent stays spent, even inside its life.
func TestASpentCodeStaysSpent(t *testing.T) {
	c := logincode.New(codeID, userID, "123456", now)
	at := now.Add(time.Minute)
	c.UsedAt = &at

	if err := c.Check("123456", now.Add(2*time.Minute)); !errors.Is(err, logincode.ErrSpent) {
		t.Errorf("Check = %v, want ErrSpent", err)
	}
}

// Six digits, because it is typed by a person from an email.
func TestAGeneratedCodeIsSixDigits(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		code, err := logincode.Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
			t.Fatalf("code = %q, want six digits", code)
		}
		seen[code] = true
	}
	if len(seen) < 40 {
		t.Errorf("50 codes produced %d distinct values", len(seen))
	}
}
