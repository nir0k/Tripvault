package domain

import (
	"slices"
	"testing"
)

// TestIdeaNormalize checks an idea's sets are cleaned up in a fixed order and
// every field is kept within its bounds.
func TestIdeaNormalize(t *testing.T) {
	five, seven, six := 5, 7, 6
	lat, lng := 66.07, -23.13
	valid := Idea{
		Title: "  Baltic tour ", Countries: []string{"ee", "LV", " lt", "EE"}, Months: []int{8, 6, 7, 6},
		DaysMin: &five, DaysMax: &seven, DaysIdeal: &six, Currency: "eur",
		Places: []IdeaPlace{{Name: " Tallinn "}, {Name: " "}, {Lat: &lat, Lng: &lng}},
		Transports: []IdeaTransport{
			{Modes: []TravelMode{ModeTrain}},
			{Modes: []TravelMode{ModeCar, ModeFlight, ModeCar}},
		},
	}
	idea, err := valid.Normalize()
	if err != nil {
		t.Fatalf("valid idea refused: %v", err)
	}
	if idea.Title != "Baltic tour" || !slices.Equal(idea.Countries, []string{"EE", "LV", "LT"}) ||
		!slices.Equal(idea.Months, []int{6, 7, 8}) || idea.Currency != "EUR" || idea.Visa != VisaNotNeeded ||
		len(idea.Places) != 2 || idea.Places[0].Name != "Tallinn" ||
		!slices.Equal(idea.Transports[1].Modes, []TravelMode{ModeCar, ModeFlight}) {
		t.Errorf("unexpected normalisation: %+v", idea)
	}

	zero, thirtyOne := 0, 31
	cases := map[string]struct {
		change func(*Idea)
		want   string
	}{
		"no title":      {func(i *Idea) { i.Title = " " }, "title:required"},
		"bad country":   {func(i *Idea) { i.Countries = []string{"FRA"} }, "countries:invalid_country"},
		"bad month":     {func(i *Idea) { i.Months = []int{13} }, "months:out_of_range"},
		"no days":       {func(i *Idea) { i.DaysMin = &zero }, "days_min:out_of_range"},
		"a month":       {func(i *Idea) { i.DaysMax = &thirtyOne }, "days_max:out_of_range"},
		"reversed days": {func(i *Idea) { i.DaysMin, i.DaysMax = &seven, &five }, "days_max:max_before_min"},
		"ideal outside": {func(i *Idea) { i.DaysIdeal = &thirtyOne; i.DaysMax = nil }, "days_ideal:out_of_range"},
		"ideal short":   {func(i *Idea) { i.DaysIdeal = &five; i.DaysMin = &six }, "days_ideal:outside_range"},
		"bad currency":  {func(i *Idea) { i.Currency = "euro" }, "currency:invalid_currency"},
		"bad visa":      {func(i *Idea) { i.Visa = "maybe" }, "visa:unsupported"},
		"bad mode":      {func(i *Idea) { i.Transports = []IdeaTransport{{Modes: []TravelMode{"rocket"}}} }, "transports:unsupported"},
		"no mode":       {func(i *Idea) { i.Transports = []IdeaTransport{{}} }, "transports:modes_out_of_range"},
		"half a point":  {func(i *Idea) { i.Places = []IdeaPlace{{Name: "X", Lat: &lat}} }, "lng:coordinates_incomplete"},
		"only a title":  {func(i *Idea) { *i = Idea{Title: "Somewhere", Currency: "EUR"} }, ""},
	}
	for name, tc := range cases {
		idea := valid
		tc.change(&idea)
		_, err := idea.Normalize()
		if got := validationCode(t, err); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// TestIdeaTotalRange checks the whole cost runs from the known parts with the
// cheapest way of getting there to them with the dearest, and is unknown
// while nothing is priced.
func TestIdeaTotalRange(t *testing.T) {
	if least, most := (Idea{}).TotalRange(); least != nil || most != nil {
		t.Error("an idea nobody priced costs something")
	}
	stay, flight, train := Money(50000), Money(30000), Money(12000)
	idea := Idea{Costs: IdeaCosts{Stay: &stay}, Transports: []IdeaTransport{
		{Modes: []TravelMode{ModeFlight}, Cost: &flight}, {Modes: []TravelMode{ModeTrain}, Cost: &train},
		{Modes: []TravelMode{ModeCar}},
	}}
	least, most := idea.TotalRange()
	if least == nil || most == nil || *least != 62000 || *most != 80000 {
		t.Errorf("range %v-%v, want 620-800", least, most)
	}
	onlyTransport := Idea{Transports: idea.Transports}
	if least, most := onlyTransport.TotalRange(); *least != 12000 || *most != 30000 {
		t.Errorf("transport alone %v-%v", *least, *most)
	}
	onlyStay := Idea{Costs: IdeaCosts{Stay: &stay}}
	if least, most := onlyStay.TotalRange(); *least != 50000 || *most != 50000 {
		t.Errorf("stay alone %v-%v", *least, *most)
	}
}
