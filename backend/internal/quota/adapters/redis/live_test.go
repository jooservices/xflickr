package redis

import (
	"context"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/quota/domain"
)

func TestLiveReadsWindowLanesAndCooldown(t *testing.T) {
	budget := generousBudget()
	budget.HourlyLimit = 10
	budget.Reserve = 0
	governor, _, client := setupGovernor(t, budget)
	for _, class := range []domain.Class{domain.Interactive, domain.Person, domain.Person} {
		if permit, _, err := acquire(t, governor, class); err != nil || permit == nil {
			t.Fatalf("acquire %s = %v %v", class, permit, err)
		}
	}
	counters := Counters{Client: client, Prefix: testPrefix}
	live, err := counters.Live(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if live.Used != 3 || live.ByClass["interactive"] != 1 || live.ByClass["person"] != 2 || time.Since(live.WindowStart) > time.Minute || !live.CooldownUntil.IsZero() {
		t.Fatalf("live = %+v", live)
	}

	until := time.Now().Add(30 * time.Minute).UnixMilli()
	if err := client.Set(context.Background(), governor.keys(1)[3], until, 0).Err(); err != nil {
		t.Fatal(err)
	}
	live, err = counters.Live(context.Background(), 1)
	if err != nil || live.CooldownUntil.UnixMilli() != until {
		t.Fatalf("cooldown = %+v %v", live, err)
	}
}

func TestLiveEmptyAndInvalid(t *testing.T) {
	_, _, client := setupGovernor(t, generousBudget())
	counters := Counters{Client: client, Prefix: testPrefix}
	live, err := counters.Live(context.Background(), 1)
	if err != nil || !live.WindowStart.IsZero() || live.Used != 0 || len(live.ByClass) != 0 {
		t.Fatalf("empty live = %+v %v", live, err)
	}
	keys := Governor{Prefix: testPrefix}.keys(1)
	for name, set := range map[string]func(){
		"used": func() { client.Set(context.Background(), keys[1], "x", 0) },
		"start": func() {
			client.Set(context.Background(), keys[1], "1", 0)
			client.Set(context.Background(), keys[2], "x", 0)
		},
		"cooldown": func() {
			client.Set(context.Background(), keys[2], "1", 0)
			client.Set(context.Background(), keys[3], "x", 0)
		},
		"class": func() {
			client.Set(context.Background(), keys[3], "1", 0)
			client.HSet(context.Background(), keys[4], "person", "x")
		},
	} {
		set()
		if _, err := counters.Live(context.Background(), 1); err == nil {
			t.Fatalf("%s: corrupt counter accepted", name)
		}
	}
	if _, err := (Counters{}).Live(context.Background(), 1); err == nil {
		t.Fatal("nil client accepted")
	}
	if _, err := counters.Live(context.Background(), 0); err == nil {
		t.Fatal("zero key accepted")
	}
	client.Close()
	if _, err := counters.Live(context.Background(), 1); err == nil {
		t.Fatal("closed client accepted")
	}
}
