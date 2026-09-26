package domain

import (
	"sort"

	"github.com/google/uuid"
)

// A place or an activity may name the member of the trip who pays for it and
// how its cost is shared: not at all, equally among the members ticked, or in
// amounts of each member's own. What the members then owe each other is worked
// out on every read of the budget and never stored, like the rest of it.
//
// A split counts only a place on a day, the same as the budget's totals: an idea
// nobody put on a day yet has not been paid for. The amount shared is the
// planned cost in a plan, and in a report what was really spent when that is
// known.

// CostSplit is how a cost is shared among the members of a trip.
type CostSplit string

// Cost splits.
const (
	// SplitNone shares nothing: whoever pays, pays for everybody.
	SplitNone CostSplit = "none"
	// SplitEveryone divides the cost equally among the members listed.
	SplitEveryone CostSplit = "everyone"
	// SplitIndividuals gives each member listed an amount of their own, which
	// together make up the cost.
	SplitIndividuals CostSplit = "individuals"
)

// maxCostShares bounds the members one cost is shared among; a trip has far
// fewer.
const maxCostShares = 100

// CostShare is one member's part of a split cost.
type CostShare struct {
	UserID uuid.UUID
	// Amount is the member's own part when the cost is split among
	// individuals, and nil when it is split equally.
	Amount *Money
}

// normalizeSplit checks the shape of a split: a known kind, a payer and at
// least one member for a split cost, no member twice, and an amount for every
// member exactly when the members have amounts of their own. Whether they are
// members of the trip, and whether the amounts add up, is CheckCostSplit's.
func (i Item) normalizeSplit() (Item, error) {
	switch i.CostSplit {
	case "":
		i.CostSplit = SplitNone
	case SplitNone, SplitEveryone, SplitIndividuals:
	default:
		return i, NewValidationError("cost_split", "unsupported", "must be none, everyone or individuals")
	}
	if i.CostSplit == SplitNone {
		i.CostShares = nil
		return i, nil
	}
	if i.PaidBy == nil {
		return i, NewValidationError("paid_by", "required", "a split cost needs somebody who pays it")
	}
	if len(i.CostShares) == 0 {
		return i, NewValidationError("cost_shares", "required", "a split cost needs somebody to share it")
	}
	if len(i.CostShares) > maxCostShares {
		return i, NewValidationError("cost_shares", "too_many", "is shared among too many people")
	}
	seen := make(map[uuid.UUID]bool, len(i.CostShares))
	shares := make([]CostShare, len(i.CostShares))
	for index, share := range i.CostShares {
		if seen[share.UserID] {
			return i, NewValidationError("cost_shares", "duplicate_member", "names somebody twice")
		}
		seen[share.UserID] = true
		switch {
		case i.CostSplit == SplitEveryone:
			share.Amount = nil
		case share.Amount == nil:
			return i, NewValidationError("cost_shares", "amount_required", "every share needs its amount")
		case *share.Amount < 0:
			return i, NewValidationError("cost_shares", "negative", "must not be negative")
		}
		shares[index] = share
	}
	i.CostShares = shares
	return i, nil
}

// SplitTotal - reports the amount a split shares among the members.
//
// Arguments:
//   - travelers: the trip's number of travellers, for a per-person cost.
//   - report: whether the item belongs to a report, where what was really
//     spent is shared once it is known.
//
// Returns:
//   - the amount for the whole group, zero without a cost.
func (i Item) SplitTotal(travelers int, report bool) Money {
	if report && i.ActualCost != nil {
		return i.ActualCostTotal(travelers)
	}
	return i.PlannedCostTotal(travelers)
}

// CheckCostSplit - checks a split against its trip: the payer and everybody
// sharing the cost must be members, and amounts of each member's own must add
// up to the cost.
//
// A member who left the trip may stay where they already were, so a place
// saved for another reason is not refused over them; allowed carries them.
//
// Arguments:
//   - allowed: the members of the trip, and anybody the stored item already named.
//   - travelers: the trip's number of travellers, for a per-person cost.
//   - report: whether the item belongs to a report.
//
// Returns:
//   - nil when the split fits the trip.
//   - a *ValidationError naming the first thing that does not.
func (i Item) CheckCostSplit(allowed map[uuid.UUID]bool, travelers int, report bool) error {
	if i.PaidBy != nil && !allowed[*i.PaidBy] {
		return NewValidationError("paid_by", "unknown_member", "is not a member of the trip")
	}
	var sum Money
	for _, share := range i.CostShares {
		if !allowed[share.UserID] {
			return NewValidationError("cost_shares", "unknown_member", "names somebody who is not a member of the trip")
		}
		if share.Amount != nil {
			sum += *share.Amount
		}
	}
	if i.CostSplit == SplitIndividuals && sum != i.SplitTotal(travelers, report) {
		return NewValidationError("cost_shares", "sum_mismatch", "the shares must add up to the cost")
	}
	return nil
}

// ShareAmounts - works out what each member sharing a cost owes, in the order
// of CostShares.
//
// An equal split gives the cents that do not divide evenly to the first
// members listed, one each, so the parts always add up to the cost.
//
// Arguments:
//   - travelers: the trip's number of travellers, for a per-person cost.
//   - report: whether the item belongs to a report.
//
// Returns:
//   - one amount per share; nil when the cost is not split.
func (i Item) ShareAmounts(travelers int, report bool) []Money {
	switch i.CostSplit {
	case SplitEveryone:
		if len(i.CostShares) == 0 {
			return nil
		}
		total := i.SplitTotal(travelers, report)
		count := Money(len(i.CostShares))
		amounts := make([]Money, len(i.CostShares))
		for index := range amounts {
			amounts[index] = total / count
			if Money(index) < total%count {
				amounts[index]++
			}
		}
		return amounts
	case SplitIndividuals:
		amounts := make([]Money, len(i.CostShares))
		for index, share := range i.CostShares {
			if share.Amount != nil {
				amounts[index] = *share.Amount
			}
		}
		return amounts
	default:
		return nil
	}
}

// MemberBalance is where one member stands across the split costs of a trip.
type MemberBalance struct {
	UserID uuid.UUID
	// Paid is what the member paid for others and themselves.
	Paid Money
	// Share is the member's own part of what was paid.
	Share Money
}

// Net is what the others owe the member, negative when the member owes them.
func (b MemberBalance) Net() Money {
	return b.Paid - b.Share
}

// Settlement is one payment that squares the members' accounts.
type Settlement struct {
	From   uuid.UUID
	To     uuid.UUID
	Amount Money
}

// BuildBalances - works out, from the split costs of a document, what every
// member paid, what their own part was, and the payments that square it.
//
// Only a cost with a payer that is split among somebody counts: the payer is
// credited with the parts of the others and of themselves, and each member is
// charged their part, so the balances always add up to zero. A place without a
// day counts nowhere, as in the budget's totals.
//
// Arguments:
//   - items: the elements of the document.
//   - travelers: the trip's number of travellers, for a per-person cost.
//   - report: whether the document is a report.
//
// Returns:
//   - the balances, in the order the members first appear.
//   - the fewest payments a greedy match finds: the largest debt paid to the
//     largest credit first.
func BuildBalances(items []Item, travelers int, report bool) ([]MemberBalance, []Settlement) {
	var balances []MemberBalance
	index := make(map[uuid.UUID]int)
	balanceOf := func(userID uuid.UUID) *MemberBalance {
		position, ok := index[userID]
		if !ok {
			position = len(balances)
			index[userID] = position
			balances = append(balances, MemberBalance{UserID: userID})
		}
		return &balances[position]
	}
	for _, item := range items {
		if !item.Kind.IsVisit() || item.DayID == nil || item.PaidBy == nil || item.CostSplit == SplitNone {
			continue
		}
		payer := *item.PaidBy
		for position, amount := range item.ShareAmounts(travelers, report) {
			balanceOf(payer).Paid += amount
			balanceOf(item.CostShares[position].UserID).Share += amount
		}
	}
	return balances, settle(balances)
}

// settle matches the members who are owed with the members who owe, largest
// first, until every account is square.
func settle(balances []MemberBalance) []Settlement {
	type party struct {
		userID uuid.UUID
		amount Money
		order  int
	}
	var creditors, debtors []party
	for order, balance := range balances {
		switch net := balance.Net(); {
		case net > 0:
			creditors = append(creditors, party{balance.UserID, net, order})
		case net < 0:
			debtors = append(debtors, party{balance.UserID, -net, order})
		}
	}
	largestFirst := func(parties []party) {
		sort.SliceStable(parties, func(a, b int) bool {
			if parties[a].amount != parties[b].amount {
				return parties[a].amount > parties[b].amount
			}
			return parties[a].order < parties[b].order
		})
	}
	largestFirst(creditors)
	largestFirst(debtors)
	settlements := []Settlement{}
	for c, d := 0, 0; c < len(creditors) && d < len(debtors); {
		amount := min(creditors[c].amount, debtors[d].amount)
		settlements = append(settlements, Settlement{From: debtors[d].userID, To: creditors[c].userID, Amount: amount})
		creditors[c].amount -= amount
		debtors[d].amount -= amount
		if creditors[c].amount == 0 {
			c++
		}
		if debtors[d].amount == 0 {
			d++
		}
	}
	return settlements
}
