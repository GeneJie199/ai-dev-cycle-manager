package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/google/uuid"
)

// Additive sidecar tables leave the v1 database and old mission audit intact.
func (s *Store) productTables(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS product_settings (id TEXT PRIMARY KEY, body TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS provider_budget (provider TEXT PRIMARY KEY,limit_micros INTEGER NOT NULL,input_rate INTEGER NOT NULL,output_rate INTEGER NOT NULL,max_calls INTEGER NOT NULL,max_output INTEGER NOT NULL,calls INTEGER NOT NULL DEFAULT 0,charged INTEGER NOT NULL DEFAULT 0,blocked INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS provider_reservations (id TEXT PRIMARY KEY,provider TEXT NOT NULL,micros INTEGER NOT NULL,input_bound INTEGER NOT NULL,output_bound INTEGER NOT NULL,settled INTEGER NOT NULL DEFAULT 0);`)
	return err
}
func (s *Store) ProductSetting(ctx context.Context, id string, result any) error {
	if err := s.productTables(ctx); err != nil {
		return err
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, "SELECT body FROM product_settings WHERE id=?", id).Scan(&raw); err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), result)
}
func (s *Store) SaveProductSetting(ctx context.Context, id string, value any) error {
	if err := s.productTables(ctx); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO product_settings(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", id, string(raw))
	return err
}
func (s *Store) ProviderBudget(ctx context.Context, id string) (models.ProviderBudget, error) {
	b := models.ProviderBudget{Provider: id}
	if err := s.productTables(ctx); err != nil {
		return b, err
	}
	err := s.db.QueryRowContext(ctx, "SELECT limit_micros,input_rate,output_rate,max_calls,max_output,calls,charged,blocked FROM provider_budget WHERE provider=?", id).Scan(&b.LimitMicros, &b.InputMicrosPerMillion, &b.OutputMicrosPerMillion, &b.MaxCalls, &b.MaxOutputTokens, &b.Calls, &b.ChargedMicros, &b.Blocked)
	if err == nil {
		err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM provider_reservations WHERE provider=? AND input_bound=1 AND output_bound=1", id).Scan(&b.MetadataCalls)
		b.GenerationCalls = b.Calls - b.MetadataCalls
	}
	return b, err
}
func (s *Store) SaveProviderBudget(ctx context.Context, b models.ProviderBudget) error {
	if err := ValidateProviderBudget(b); err != nil {
		return err
	}
	if err := s.productTables(ctx); err != nil {
		return err
	}
	// Counters/blocked flags are owned by the ledger, never reset by a configuration update.
	_, err := s.db.ExecContext(ctx, `INSERT INTO provider_budget(provider,limit_micros,input_rate,output_rate,max_calls,max_output) VALUES(?,?,?,?,?,?) ON CONFLICT(provider) DO UPDATE SET limit_micros=excluded.limit_micros,input_rate=excluded.input_rate,output_rate=excluded.output_rate,max_calls=excluded.max_calls,max_output=excluded.max_output`, b.Provider, b.LimitMicros, b.InputMicrosPerMillion, b.OutputMicrosPerMillion, b.MaxCalls, b.MaxOutputTokens)
	return err
}
func (s *Store) ReserveProvider(ctx context.Context, id string, input, output int) (models.ProviderReservation, error) {
	b, err := s.ProviderBudget(ctx, id)
	if err != nil {
		return models.ProviderReservation{}, fmt.Errorf("configuration required: explicit provider budget: %w", err)
	}
	if input < 1 || input > 1000000 || output < 1 || output > b.MaxOutputTokens {
		return models.ProviderReservation{}, errors.New("token bound exceeds configured budget")
	}
	amount := (int64(input)*b.InputMicrosPerMillion + int64(output)*b.OutputMicrosPerMillion + 999999) / 1000000
	reservation := models.ProviderReservation{ID: uuid.NewString(), Provider: id, Micros: amount, InputBound: input, OutputBound: output, InputRate: b.InputMicrosPerMillion, OutputRate: b.OutputMicrosPerMillion}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return reservation, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE provider_budget SET calls=calls+1,charged=charged+? WHERE provider=? AND blocked=0 AND calls<max_calls AND charged+?<=limit_micros AND input_rate=? AND output_rate=? AND max_output>=?", amount, id, amount, b.InputMicrosPerMillion, b.OutputMicrosPerMillion, output)
	if err != nil {
		return reservation, err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return reservation, errors.New("budget exhausted or uncertain usage blocked this provider; no request sent")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO provider_reservations(id,provider,micros,input_bound,output_bound) VALUES(?,?,?,?,?)", reservation.ID, id, amount, input, output); err != nil {
		return reservation, err
	}
	return reservation, tx.Commit()
}
func (s *Store) SettleProvider(ctx context.Context, r models.ProviderReservation, input, output int, known bool) error {
	_, err := s.ProviderBudget(ctx, r.Provider)
	if err != nil {
		return err
	}
	valid := known && input >= 0 && output >= 0 && input <= r.InputBound && output <= r.OutputBound
	// Rates can be edited while a request is in flight: never refund based on a different tariff.
	charged := r.Micros
	if valid {
		cost := (int64(input)*r.InputRate + int64(output)*r.OutputRate + 999999) / 1000000
		if cost <= charged {
			charged = cost
		} else {
			valid = false
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE provider_reservations SET settled=1 WHERE id=? AND provider=? AND settled=0", r.ID, r.Provider)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errors.New("reservation already settled or missing")
	}
	if !valid {
		_, err = tx.ExecContext(ctx, "UPDATE provider_budget SET blocked=1 WHERE provider=?", r.Provider)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE provider_budget SET charged=charged-? WHERE provider=?", r.Micros-charged, r.Provider)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if !valid {
		return errors.New("provider usage missing or outside reserved bound: full reservation charged; further requests blocked")
	}
	return nil
}

// A human may accept full reserved cost and retry with the remaining limits; counters never reset.
func (s *Store) ResumeProviderBudget(ctx context.Context, id string) error {
	if err := s.productTables(ctx); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE provider_budget SET blocked=0 WHERE provider=? AND blocked=1 AND NOT EXISTS(SELECT 1 FROM provider_reservations WHERE provider=? AND settled=0)", id, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return errors.New("no settled uncertain-usage block to acknowledge; active reservations cannot be cleared")
	}
	return nil
}

func ValidateProviderBudget(b models.ProviderBudget) error {
	if b.Provider == "" || b.LimitMicros <= 0 || b.LimitMicros > 1000000000 || b.InputMicrosPerMillion <= 0 || b.InputMicrosPerMillion > 1000000000 || b.OutputMicrosPerMillion <= 0 || b.OutputMicrosPerMillion > 1000000000 || b.MaxCalls < 1 || b.MaxCalls > 30 || b.MaxOutputTokens < 64 || b.MaxOutputTokens > 8192 {
		return errors.New("explicit positive CNY limit, conservative input/output prices, 1–30 calls and 64–8192 output tokens required")
	}
	return nil
}
