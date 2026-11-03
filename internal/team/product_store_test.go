package team

import (
	"context"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testBudget(id string) models.ProviderBudget {
	return models.ProviderBudget{Provider: id, LimitMicros: 5000000, InputMicrosPerMillion: 2000000, OutputMicrosPerMillion: 8000000, MaxCalls: 3, MaxOutputTokens: 512}
}
func TestProviderBudgetAtomicLimitsPersistAndNeverReset(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "team.db")
	s, e := Open(file)
	if e != nil {
		t.Fatal(e)
	}
	b := testBudget("fixture")
	if e = s.SaveProviderBudget(ctx, b); e != nil {
		t.Fatal(e)
	}
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.ReserveProvider(ctx, "fixture", 100, 100)
			if err == nil {
				count.Add(1)
				if err = s.SettleProvider(ctx, r, 50, 50, true); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if count.Load() != 3 {
		t.Fatalf("authorized %d requests, want 3", count.Load())
	}
	before, e := s.ProviderBudget(ctx, "fixture")
	if e != nil || before.Calls != 3 || before.ChargedMicros != 1500 {
		t.Fatalf("ledger %+v %v", before, e)
	}
	if e = s.SaveProviderBudget(ctx, b); e != nil {
		t.Fatal(e)
	}
	after, _ := s.ProviderBudget(ctx, "fixture")
	if after.Calls != before.Calls || after.ChargedMicros != before.ChargedMicros {
		t.Fatal("budget update reset ledger")
	}
	s.Close()
	s, e = Open(file)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.ReserveProvider(ctx, "fixture", 1, 1); e == nil {
		t.Fatal("restart bypassed call cap")
	}
}
func TestProviderBudgetCNYCapAndUnknownUsageFailClosed(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "team.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	b := testBudget("fixture")
	b.LimitMicros = 1500
	b.MaxCalls = 30
	s.SaveProviderBudget(ctx, b)
	r, e := s.ReserveProvider(ctx, "fixture", 100, 100)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReserveProvider(ctx, "fixture", 100, 100); e == nil {
		t.Fatal("CNY cap bypassed")
	}
	if e = s.SettleProvider(ctx, r, 0, 0, false); e == nil {
		t.Fatal("missing usage accepted")
	}
	after, _ := s.ProviderBudget(ctx, "fixture")
	if !after.Blocked || after.ChargedMicros != r.Micros {
		t.Fatalf("reservation refunded: %+v", after)
	}
	b.LimitMicros = 5000000
	s.SaveProviderBudget(ctx, b)
	if _, e = s.ReserveProvider(ctx, "fixture", 1, 1); e == nil {
		t.Fatal("policy update cleared uncertain usage block")
	}
	if e = s.SettleProvider(ctx, r, 1, 1, true); e == nil {
		t.Fatal("double settlement accepted")
	}
}
func TestProviderReservationRetainsOriginalTariff(t *testing.T) {
	ctx := context.Background()
	s, e := Open(filepath.Join(t.TempDir(), "team.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	b := testBudget("fixture")
	s.SaveProviderBudget(ctx, b)
	r, e := s.ReserveProvider(ctx, b.Provider, 100, 100)
	if e != nil {
		t.Fatal(e)
	}
	b.InputMicrosPerMillion = 1
	b.OutputMicrosPerMillion = 1
	s.SaveProviderBudget(ctx, b)
	if e = s.SettleProvider(ctx, r, 50, 50, true); e != nil {
		t.Fatal(e)
	}
	after, _ := s.ProviderBudget(ctx, b.Provider)
	if after.ChargedMicros != 500 {
		t.Fatalf("changed tariff undercharged in-flight request: %+v", after)
	}
}

func TestUnsettledProviderReservationRecoveryKeepsSpendAndBlocks(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "ledger.db")
	s, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	s.SaveProviderBudget(ctx, testBudget("fixture"))
	reservation, err := s.ReserveProvider(ctx, "fixture", 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	budget, err := s.ProviderBudget(ctx, "fixture")
	if err != nil || !budget.Blocked || budget.Calls != 1 || budget.ChargedMicros != reservation.Micros {
		t.Fatalf("restart lost uncertainty/spend: %+v %v", budget, err)
	}
	if _, err = s.ReserveProvider(ctx, "fixture", 1, 1); err == nil {
		t.Fatal("restart sent another request with unresolved usage")
	}
}
func TestProviderLedgerLeaseHelper(t *testing.T) {
	file := os.Getenv("DEVCYCLE_PROVIDER_LEDGER_HELPER")
	if file == "" {
		return
	}
	s, err := Open(file)
	if err == nil {
		s.Close()
		os.Exit(2)
	}
	os.Exit(0)
}
func TestProviderLedgerRejectsSecondServerProcess(t *testing.T) {
	file := filepath.Join(t.TempDir(), "ledger.db")
	s, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestProviderLedgerLeaseHelper$")
	cmd.Env = append(os.Environ(), "DEVCYCLE_PROVIDER_LEDGER_HELPER="+file)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("second process obtained ledger ownership or failed unexpectedly: %v %s", err, out)
	}
}
