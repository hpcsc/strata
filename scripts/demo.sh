#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
demo=$(mktemp -d "${TMPDIR:-/tmp}/strata-demo.XXXXXX")
trap 'rm -rf "$demo"' EXIT

export GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.com
export GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.com
export GIT_CONFIG_COUNT=3
export GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false
export GIT_CONFIG_KEY_1=core.hooksPath GIT_CONFIG_VALUE_1=/dev/null
export GIT_CONFIG_KEY_2=init.defaultBranch GIT_CONFIG_VALUE_2=main

cd "$demo"
git init -q --bare origin.git
git clone -q origin.git repo 2>/dev/null
cd repo

write() { mkdir -p "$(dirname "$1")" && cat >"$1"; }
save() { git add -A && git commit -qm "$1"; }

write go.mod <<'EOF'
module example.com/shop

go 1.23
EOF
write orders/order.go <<'EOF'
package orders

type Item struct {
	SKU      string
	Quantity int
	Price    int
}

type Order struct {
	ID     string
	Items  []Item
	Status string
}

func (o *Order) Total() int {
	total := 0
	for _, item := range o.Items {
		total += item.Price * item.Quantity
	}
	return total
}
EOF
write README.md <<'EOF'
# shop

A small shop service.
EOF
save "Start the shop service"
git push -q origin main 2>/dev/null
git remote set-head origin -a >/dev/null

git switch -qc orders-events
write orders/events.go <<'EOF'
package orders

import "time"

type OrderPlaced struct {
	OrderID  string
	Total    int
	PlacedAt time.Time
}

type OrderCancelled struct {
	OrderID     string
	Reason      string
	CancelledAt time.Time
}
EOF
save "Name the events an order records"
write orders/order.go <<'EOF'
package orders

import "time"

type Item struct {
	SKU      string
	Quantity int
	Price    int
}

type Order struct {
	ID     string
	Items  []Item
	Status string
	events []any
}

func (o *Order) Total() int {
	total := 0
	for _, item := range o.Items {
		total += item.Price * item.Quantity
	}
	return total
}

func (o *Order) Place(now time.Time) {
	o.Status = "placed"
	o.events = append(o.events, OrderPlaced{OrderID: o.ID, Total: o.Total(), PlacedAt: now})
}

func (o *Order) Cancel(reason string, now time.Time) {
	o.Status = "cancelled"
	o.events = append(o.events, OrderCancelled{OrderID: o.ID, Reason: reason, CancelledAt: now})
}
EOF
save "Record an event when an order is placed or cancelled"

git switch -qc orders-handler
write orders/handler.go <<'EOF'
package orders

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("order not found")

type Store interface {
	Load(id string) (*Order, error)
	Save(o *Order) error
}

type Handler struct {
	store Store
	now   func() time.Time
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store, now: time.Now}
}

func (h *Handler) Cancel(id, reason string) error {
	order, err := h.store.Load(id)
	if err != nil {
		return err
	}
	order.Cancel(reason, h.now())
	return h.store.Save(order)
}
EOF
save "Cancel an order through the handler"
write orders/handler_test.go <<'EOF'
package orders

import "testing"

type memoryStore map[string]*Order

func (m memoryStore) Load(id string) (*Order, error) {
	if o, ok := m[id]; ok {
		return o, nil
	}
	return nil, ErrNotFound
}

func (m memoryStore) Save(o *Order) error {
	m[o.ID] = o
	return nil
}

func TestHandler(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		t.Run("marks the order cancelled", func(t *testing.T) {
			store := memoryStore{"o1": {ID: "o1"}}
			if err := NewHandler(store).Cancel("o1", "changed mind"); err != nil {
				t.Fatal(err)
			}
			if store["o1"].Status != "cancelled" {
				t.Fatalf("status = %q", store["o1"].Status)
			}
		})
	})
}
EOF
save "Test cancelling through the handler"

git switch -qc orders-api
write api/routes.go <<'EOF'
package api

import (
	"net/http"

	"example.com/shop/orders"
)

func Routes(h *orders.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if err := h.Cancel(r.PathValue("id"), r.FormValue("reason")); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
EOF
write README.md <<'EOF'
# shop

A small shop service.

## API

- `POST /orders/{id}/cancel` cancels an order. Send the reason as `reason`.
EOF
save "Expose order cancellation over HTTP"

git switch -q orders-events
perl -0pi -e 's/\to.Status = "cancelled"\n/\tif o.Status == "cancelled" {\n\t\treturn\n\t}\n\to.Status = "cancelled"\n/' orders/order.go
save "Do not cancel an order twice"

git switch -q main
git switch -qc billing-invoice
write billing/invoice.go <<'EOF'
package billing

type Invoice struct {
	Number  string
	OrderID string
	Amount  int
}
EOF
save "Add the invoice"
git worktree add -q -b billing-pdf ../billing-pdf-worktree "$(git rev-parse HEAD)"
(
  cd ../billing-pdf-worktree
  write billing/pdf.go <<'EOF'
package billing

import "fmt"

func (i Invoice) PDFName() string {
	return fmt.Sprintf("invoice-%s.pdf", i.Number)
}
EOF
  save "Name the invoice PDF"
)
git mv README.md OVERVIEW.md
save "Rename the readme"

git switch -q orders-handler
"$root/bin/strata" "$@"
