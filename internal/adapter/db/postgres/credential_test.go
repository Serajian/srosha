//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// theSecret is shaped like what the crypto layer will store: a value that names
// the key that produced it. Nothing here reads it, which is the point.
const theSecret = "v1.2.bm9uY2U.Y2lwaGVydGV4dA"

func aCredential(t *testing.T, id, sourceID, name string, isDefault bool) *credential.Credential {
	t.Helper()

	c, err := credential.New(
		shared.ID(id), sourceID, shared.ChannelEmail, name, isDefault,
		time.Now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		t.Fatalf("credential.New: %v", err)
	}
	return c
}

// withASource gives the credentials something to hang off: source_id is a
// foreign key, so every test here needs one first.
func withASource(t *testing.T, pool *pgxpool.Pool, suffix string) string {
	t.Helper()

	src := aSource(ulid(suffix))
	if err := postgres.NewSourceRepository(pool).Create(context.Background(), src); err != nil {
		t.Fatalf("Create source: %v", err)
	}
	return src.ID
}

// switchOff does by hand what no query does: there is no deactivate statement
// yet, and these tests are about what happens once a credential is off.
func switchOff(t *testing.T, pool *pgxpool.Pool, id string) {
	t.Helper()

	_, err := pool.Exec(context.Background(),
		"UPDATE credentials SET is_active = FALSE, is_default = FALSE WHERE id = $1", id)
	if err != nil {
		t.Fatalf("switch off: %v", err)
	}
}

func TestCredentialsRoundTrip(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "C0")
	repo := postgres.NewCredentialRepository(pool)
	ctx := context.Background()

	want := aCredential(t, ulid("C1"), sourceID, "transactional", true)
	if err := repo.Create(ctx, want, []byte(`{"host":"smtp.acme.test"}`), theSecret); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ListBySourceAndChannel(ctx, sourceID, shared.ChannelEmail)
	if err != nil {
		t.Fatalf("ListBySourceAndChannel: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d credentials, want 1", len(got))
	}

	c := got[0]
	if c.ID != want.ID || c.Name != want.Name || c.SourceID != sourceID {
		t.Errorf("identity did not survive: %+v", c)
	}
	if c.Channel != shared.ChannelEmail {
		t.Errorf("Channel = %q, want email", c.Channel)
	}
	if !c.IsDefault() || !c.IsActive() {
		t.Errorf("flags = default %v / active %v, want both true", c.IsDefault(), c.IsActive())
	}
	if !c.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", c.CreatedAt, want.CreatedAt)
	}
}

// A source picks a credential by name, so two by one name on one channel would
// make that choice ambiguous. The index refuses it and this reports it as what
// it is rather than as a broken database.
func TestTwoCredentialsCannotShareANameOnAChannel(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "C9")
	repo := postgres.NewCredentialRepository(pool)
	ctx := context.Background()

	first := aCredential(t, ulid("CA"), sourceID, "transactional", true)
	if err := repo.Create(ctx, first, nil, theSecret); err != nil {
		t.Fatalf("Create: %v", err)
	}

	second := aCredential(t, ulid("CB"), sourceID, "transactional", false)
	err := repo.Create(ctx, second, nil, theSecret)
	if !errs.IsType(err, errs.ErrDuplicateEntry) {
		t.Fatalf("Create() = %v, want a duplicate", err)
	}
}

// The list is what the core sees, and the core must never see a token. The
// entity has no field for one, so the only way it could travel is a mapper
// putting it somewhere -- which this would catch.
func TestTheSecretDoesNotTravelWithTheIdentity(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "C2")
	repo := postgres.NewCredentialRepository(pool)
	ctx := context.Background()

	cred := aCredential(t, ulid("C3"), sourceID, "transactional", true)
	if err := repo.Create(ctx, cred, []byte(`{"host":"smtp.acme.test"}`), theSecret); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ListBySourceAndChannel(ctx, sourceID, shared.ChannelEmail)
	if err != nil {
		t.Fatalf("ListBySourceAndChannel: %v", err)
	}
	if printed := fmt.Sprintf("%+v", got); strings.Contains(printed, theSecret) ||
		strings.Contains(printed, "smtp.acme.test") {
		t.Errorf("the list carried the secret or the provider settings: %s", printed)
	}

	config, secret, err := repo.ReadMaterial(ctx, cred.ID)
	if err != nil {
		t.Fatalf("ReadMaterial: %v", err)
	}
	if secret != theSecret {
		t.Errorf("secret = %q, want it back exactly as stored", secret)
	}
	if !strings.Contains(string(config), "smtp.acme.test") {
		t.Errorf("config = %s, want the provider settings", config)
	}
}

// Pick has a branch that says "that one is switched off". Filtering in SQL would
// make that branch unreachable and answer "no such credential" instead, sending
// the source to look for a typo it does not have.
func TestListKeepsSwitchedOffCredentials(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "C4")
	repo := postgres.NewCredentialRepository(pool)
	ctx := context.Background()

	live := aCredential(t, ulid("C5"), sourceID, "transactional", true)
	dead := aCredential(t, ulid("C6"), sourceID, "marketing", false)
	for _, c := range []*credential.Credential{live, dead} {
		if err := repo.Create(ctx, c, nil, theSecret); err != nil {
			t.Fatalf("Create %q: %v", c.Name, err)
		}
	}
	switchOff(t, pool, dead.ID.String())

	got, err := repo.ListBySourceAndChannel(ctx, sourceID, shared.ChannelEmail)
	if err != nil {
		t.Fatalf("ListBySourceAndChannel: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d credentials, want both", len(got))
	}

	if _, err := credential.Pick(got, "marketing"); !errors.Is(err, credential.ErrInactive) {
		t.Errorf("Pick(marketing) = %v, want ErrInactive", err)
	}
	if chosen, err := credential.Pick(got, ""); err != nil || chosen.Name != "transactional" {
		t.Errorf("Pick(default) = %+v, %v", chosen, err)
	}
}

// The identity was chosen a moment ago and switched off since. The send path
// asks again here rather than trusting that earlier read, because the two happen
// at different times.
func TestReadMaterialRefusesASwitchedOffCredential(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "C7")
	repo := postgres.NewCredentialRepository(pool)
	ctx := context.Background()

	cred := aCredential(t, ulid("C8"), sourceID, "transactional", true)
	if err := repo.Create(ctx, cred, nil, theSecret); err != nil {
		t.Fatalf("Create: %v", err)
	}
	switchOff(t, pool, cred.ID.String())

	_, _, err := repo.ReadMaterial(ctx, cred.ID)
	if !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("ReadMaterial() = %v, want ErrNotFound", err)
	}
	if !errs.IsType(err, errs.ErrNotFound) {
		t.Errorf("type = %v, want not found", errs.TypeOf(err))
	}
}

// Moving the default is two writes and neither is safe alone: without the clear
// the index refuses the new one, and without the new one the channel is left
// with no default at all.
func TestMovingTheDefaultTakesBothHalves(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "CC")
	repo := postgres.NewCredentialRepository(pool)
	uow := postgres.NewUnitOfWork(pool)
	ctx := context.Background()

	old := aCredential(t, ulid("CD"), sourceID, "transactional", true)
	if err := repo.Create(ctx, old, nil, theSecret); err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("the second default alone is refused", func(t *testing.T) {
		rival := aCredential(t, ulid("CE"), sourceID, "marketing", true)
		if err := repo.Create(ctx, rival, nil, theSecret); !errs.IsType(err, errs.ErrDuplicateEntry) {
			t.Fatalf("Create() = %v, want a duplicate", err)
		}
	})

	t.Run("both halves together move it", func(t *testing.T) {
		fresh := aCredential(t, ulid("CF"), sourceID, "marketing", true)
		err := uow.Atomically(ctx, func(ctx context.Context) error {
			if err := repo.ClearDefault(ctx, sourceID, shared.ChannelEmail, fresh.UpdatedAt); err != nil {
				return err
			}
			return repo.Create(ctx, fresh, nil, theSecret)
		})
		if err != nil {
			t.Fatalf("Atomically: %v", err)
		}

		got, err := repo.ListBySourceAndChannel(ctx, sourceID, shared.ChannelEmail)
		if err != nil {
			t.Fatalf("ListBySourceAndChannel: %v", err)
		}

		var defaults []string
		for _, c := range got {
			if c.IsDefault() {
				defaults = append(defaults, c.Name)
			}
		}
		if len(defaults) != 1 || defaults[0] != "marketing" {
			t.Errorf("defaults = %v, want only marketing", defaults)
		}
	})
}

// The first credential on a channel finds no default to clear, and that is the
// ordinary case rather than a failure.
func TestClearingNoDefaultIsNotAFailure(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "CG")
	repo := postgres.NewCredentialRepository(pool)

	err := repo.ClearDefault(
		context.Background(), sourceID, shared.ChannelTelegram, time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("ClearDefault() = %v, want nil", err)
	}
}

// Reseal is the write that makes a key change cost no outage. It rewrites the
// secret and nothing else, and only if the row still holds what the caller read.
func TestResealReplacesOnlyTheSecret(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "CR")
	repo := postgres.NewCredentialRepository(pool)
	ctx := context.Background()

	c := aCredential(t, ulid("CR1"), sourceID, "transactional", true)
	config := []byte(`{"host":"smtp.acme.test"}`)
	if err := repo.Create(ctx, c, config, theSecret); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const underTheNewKey = "v1.3.bm9uY2Uy.Y2lwaGVydGV4dDI"

	written, err := repo.Reseal(ctx, c.ID, theSecret, underTheNewKey, time.Now().UTC())
	if err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	if !written {
		t.Fatal("Reseal reported no write for a row that was there")
	}

	gotConfig, gotSecret, err := repo.ReadMaterial(ctx, c.ID)
	if err != nil {
		t.Fatalf("ReadMaterial: %v", err)
	}
	if gotSecret != underTheNewKey {
		t.Errorf("secret = %q, want the resealed value", gotSecret)
	}
	// jsonb reformats, so the bytes are compared as json rather than as text.
	if !sameJSON(t, gotConfig, config) {
		t.Errorf("config = %s, want %s", gotConfig, config)
	}

	// The identity itself must be exactly as it was.
	list, err := repo.ListBySourceAndChannel(ctx, sourceID, shared.ChannelEmail)
	if err != nil {
		t.Fatalf("ListBySourceAndChannel: %v", err)
	}
	if len(list) != 1 || list[0].Name != "transactional" || !list[0].IsDefault() || !list[0].IsActive() {
		t.Errorf("the identity moved: %+v", list)
	}
}

// Two senders reading one credential at the same moment both reseal. Only one
// of them can be the row, and the loser must write nothing rather than put its
// own value over the winner's.
func TestAResealThatLostTheRaceWritesNothing(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "CL")
	repo := postgres.NewCredentialRepository(pool)
	ctx := context.Background()

	c := aCredential(t, ulid("CL1"), sourceID, "transactional", false)
	if err := repo.Create(ctx, c, nil, theSecret); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const winner = "v1.3.d2lubmVy.d2lubmVy"
	if _, err := repo.Reseal(ctx, c.ID, theSecret, winner, time.Now().UTC()); err != nil {
		t.Fatalf("Reseal: %v", err)
	}

	// The loser still holds the value it read a moment ago.
	written, err := repo.Reseal(ctx, c.ID, theSecret, "v1.3.bG9zZXI.bG9zZXI", time.Now().UTC())
	if err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	if written {
		t.Error("the loser overwrote the winner")
	}

	_, got, err := repo.ReadMaterial(ctx, c.ID)
	if err != nil {
		t.Fatalf("ReadMaterial: %v", err)
	}
	if got != winner {
		t.Errorf("secret = %q, want the winner's value", got)
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()

	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("unmarshal %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return reflect.DeepEqual(x, y)
}
