package domain

import (
	"testing"

	"github.com/google/uuid"
)

// cents turns an amount in hundredths into a stored amount.
func cents(value int64) *Money {
	amount := Money(value)
	return &amount
}

// splitPlace builds a place on a day costing the given hundredths, paid by
// payer and split among the members.
func splitPlace(payer uuid.UUID, total int64, split CostSplit, shares ...CostShare) Item {
	day := uuid.New()
	return Item{ID: uuid.New(), DayID: &day, Kind: ItemPlace, Name: "Boat", Category: CategoryOther,
		PlannedCost: cents(total), PaidBy: &payer, CostSplit: split, CostShares: shares}
}

func TestNormalizeSplit(t *testing.T) {
	ann, bob := uuid.New(), uuid.New()
	cases := []struct {
		name string
		item Item
		code string
	}{
		{"no split drops the shares", Item{CostShares: []CostShare{{UserID: ann}}}, ""},
		{"unknown split", Item{CostSplit: "halves"}, "cost_split:unsupported"},
		{"split without a payer", Item{CostSplit: SplitEveryone, CostShares: []CostShare{{UserID: ann}}}, "paid_by:required"},
		{"split among nobody", Item{CostSplit: SplitEveryone, PaidBy: &ann}, "cost_shares:required"},
		{"member twice", Item{CostSplit: SplitEveryone, PaidBy: &ann,
			CostShares: []CostShare{{UserID: bob}, {UserID: bob}}}, "cost_shares:duplicate_member"},
		{"individual without an amount", Item{CostSplit: SplitIndividuals, PaidBy: &ann,
			CostShares: []CostShare{{UserID: bob}}}, "cost_shares:amount_required"},
		{"equal split", Item{CostSplit: SplitEveryone, PaidBy: &ann,
			CostShares: []CostShare{{UserID: ann, Amount: cents(5)}, {UserID: bob}}}, ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			item, err := test.item.normalizeSplit()
			if code := validationCode(t, err); code != test.code {
				t.Fatalf("code = %q, want %q (%v)", code, test.code, err)
			}
			if err != nil {
				return
			}
			if item.CostSplit == SplitNone && item.CostShares != nil {
				t.Fatalf("shares kept without a split: %v", item.CostShares)
			}
			for _, share := range item.CostShares {
				if item.CostSplit == SplitEveryone && share.Amount != nil {
					t.Fatalf("an equal share kept its amount")
				}
			}
		})
	}
}

func TestCheckCostSplit(t *testing.T) {
	ann, bob, eve := uuid.New(), uuid.New(), uuid.New()
	members := map[uuid.UUID]bool{ann: true, bob: true}

	fair := splitPlace(ann, 1000, SplitIndividuals, CostShare{UserID: ann, Amount: cents(400)},
		CostShare{UserID: bob, Amount: cents(600)})
	if err := fair.CheckCostSplit(members, 2, false); err != nil {
		t.Fatalf("shares adding up refused: %v", err)
	}
	short := splitPlace(ann, 1000, SplitIndividuals, CostShare{UserID: ann, Amount: cents(400)})
	if code := validationCode(t, short.CheckCostSplit(members, 2, false)); code != "cost_shares:sum_mismatch" {
		t.Fatalf("code = %q, want sum_mismatch", code)
	}
	stranger := splitPlace(ann, 1000, SplitEveryone, CostShare{UserID: eve})
	if code := validationCode(t, stranger.CheckCostSplit(members, 2, false)); code != "cost_shares:unknown_member" {
		t.Fatalf("code = %q, want unknown_member", code)
	}
	payer := splitPlace(eve, 1000, SplitEveryone, CostShare{UserID: ann})
	if code := validationCode(t, payer.CheckCostSplit(members, 2, false)); code != "paid_by:unknown_member" {
		t.Fatalf("code = %q, want unknown_member", code)
	}

	// A per-person cost is shared for the whole group, and a report shares
	// what was really spent.
	perPerson := splitPlace(ann, 500, SplitIndividuals, CostShare{UserID: ann, Amount: cents(500)},
		CostShare{UserID: bob, Amount: cents(500)})
	perPerson.CostPerPerson = true
	if err := perPerson.CheckCostSplit(members, 2, false); err != nil {
		t.Fatalf("per-person shares refused: %v", err)
	}
	perPerson.ActualCost = cents(600)
	if code := validationCode(t, perPerson.CheckCostSplit(members, 2, true)); code != "cost_shares:sum_mismatch" {
		t.Fatalf("code = %q, want sum_mismatch against the actual cost", code)
	}
}

func TestShareAmountsGivesLeftoverCentsToTheFirst(t *testing.T) {
	ann, bob, eve := uuid.New(), uuid.New(), uuid.New()
	item := splitPlace(ann, 1000, SplitEveryone, CostShare{UserID: ann}, CostShare{UserID: bob}, CostShare{UserID: eve})
	got := item.ShareAmounts(1, false)
	want := []Money{334, 333, 333}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("amounts = %v, want %v", got, want)
		}
	}
}

func TestBuildBalances(t *testing.T) {
	ann, bob, eve := uuid.New(), uuid.New(), uuid.New()
	boat := splitPlace(ann, 9000, SplitEveryone, CostShare{UserID: ann}, CostShare{UserID: bob}, CostShare{UserID: eve})
	dinner := splitPlace(bob, 3000, SplitIndividuals, CostShare{UserID: ann, Amount: cents(1000)},
		CostShare{UserID: eve, Amount: cents(2000)})
	// Neither an idea without a day nor a cost nobody splits counts.
	idea := splitPlace(eve, 5000, SplitEveryone, CostShare{UserID: ann})
	idea.DayID = nil
	lunch := splitPlace(eve, 5000, SplitNone)

	balances, settlements := BuildBalances([]Item{boat, dinner, idea, lunch}, 3, false)

	net := map[uuid.UUID]Money{}
	var total Money
	for _, balance := range balances {
		net[balance.UserID] = balance.Net()
		total += balance.Net()
	}
	if total != 0 {
		t.Fatalf("balances add up to %d, want 0", total)
	}
	// Ann paid 90 and owes 30 + 10; Bob paid 30 and owes 30; Eve paid nothing and owes 30 + 20.
	if net[ann] != 5000 || net[bob] != 0 || net[eve] != -5000 {
		t.Fatalf("net = ann %d, bob %d, eve %d", net[ann], net[bob], net[eve])
	}
	if len(settlements) != 1 || settlements[0] != (Settlement{From: eve, To: ann, Amount: 5000}) {
		t.Fatalf("settlements = %+v", settlements)
	}
}

func TestSettleMatchesLargestFirst(t *testing.T) {
	ann, bob, eve, joe := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	settlements := settle([]MemberBalance{
		{UserID: ann, Paid: 7000},
		{UserID: bob, Paid: 1000},
		{UserID: eve, Share: 5000},
		{UserID: joe, Share: 3000},
	})
	want := []Settlement{
		{From: eve, To: ann, Amount: 5000},
		{From: joe, To: ann, Amount: 2000},
		{From: joe, To: bob, Amount: 1000},
	}
	if len(settlements) != len(want) {
		t.Fatalf("settlements = %+v, want %+v", settlements, want)
	}
	for index := range want {
		if settlements[index] != want[index] {
			t.Fatalf("settlements = %+v, want %+v", settlements, want)
		}
	}
}
