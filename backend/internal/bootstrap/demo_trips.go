package bootstrap

import "github.com/nir0k/tripvault/backend/internal/domain"

// The two example trips, as data. One has been travelled and written up, the
// other is still being planned, so both sides of the product have something to
// look at.
//
// The dates are fixed rather than relative to today: a trip whose dates moved on
// every start would change its status from one run to the next, and the example
// is meant to look the same every time. The travelled one sits in the past so it
// reads as finished; the planned one sits far enough ahead that it keeps reading
// as upcoming for a good while.
//
// Move the planned trip forward once that date passes: a trip still being planned
// that the list calls "completed" is the wrong thing to show.

// icelandTrip is the trip that has been travelled: a plan and a full report.
func icelandTrip() demoTrip {
	return demoTrip{
		Title: "Iceland: the south coast",
		Summary: "Four days along route 1, from Reykjavík to Vík and back. Waterfalls, black sand " +
			"and one very windy afternoon.",
		Start: "2026-06-20", End: "2026-06-23", Timezone: "Atlantic/Reykjavik",
		Currency: "EUR", Travelers: 2, Budget: "1800",

		Stays: []demoStay{{
			Name: "Guesthouse Hraun", Kind: domain.StayApartment,
			Address: "Hvolsvegur 12, Hvolsvöllur", Lat: 63.7492, Lng: -20.2264,
			CheckIn: "2026-06-20", CheckOut: "2026-06-22", CheckInAt: "15:00", CheckOutAt: "10:00",
			BookingRef: "HR-884213", URL: "https://example.com/hraun", Contacts: "+354 555 0143",
			Notes: "Parking behind the house. The key is in the box, code in the email.",
			Cost:  "320", ActualCost: "320",
		}, {
			Name: "Vík Sea View", Kind: domain.StayHotel,
			Address: "Klettsvegur 3, Vík í Mýrdal", Lat: 63.4187, Lng: -19.0060,
			CheckIn: "2026-06-22", CheckOut: "2026-06-23", CheckInAt: "16:00", CheckOutAt: "11:00",
			BookingRef: "VSV-20260622", URL: "https://example.com/vik-sea-view",
			Contacts: "stay@example.com", Notes: "Breakfast from 07:30. Ask for a room facing the sea.",
			Cost: "410", ActualCost: "430",
		}},

		Days: []demoDay{{
			Title: "Reykjavík to Hvolsvöllur",
			Notes: "Pick the car up at the airport and take route 1 east. Nothing ambitious: the point " +
				"is to be at the guesthouse before the shop closes.",
			Places: []demoPlace{{
				Name: "Keflavík airport", Category: domain.CategoryTransport,
				Lat: 63.9850, Lng: -22.6056, Address: "Keflavíkurflugvöllur, 235 Reykjanesbær",
				URL: "https://example.com/kef", BookingRef: "CAR-4471",
				Description: "Car hire desk is in the arrivals hall, on the right. **Bring the booking number.**",
				DesiredTime: "10:30", VisitMinutes: 45, CostCategory: domain.CostTransport,
				Report: &demoReport{Status: domain.StatusVisited, ActualTime: "10:55",
					Story: "The queue took half an hour; the car had a scratch we photographed."},
			}, {
				Name: "Seljalandsfoss", Category: domain.CategoryNature,
				Lat: 63.6156, Lng: -19.9928, Address: "Route 249, Hvolsvöllur",
				URL:         "https://example.com/seljalandsfoss",
				Description: "The path goes *behind* the waterfall. Waterproofs, not umbrellas.",
				DesiredTime: "15:00", VisitMinutes: 60, Cost: "8", PerPerson: true,
				CostCategory: domain.CostActivities,
				Report: &demoReport{Status: domain.StatusVisited, Rating: 5, ActualTime: "15:20",
					ActualCost: "8",
					Story:      "Soaked through in two minutes and worth every bit of it. Go early or late; at three it was full."},
			}, {
				Name: "Gljúfrabúi", Category: domain.CategorySight,
				Lat: 63.6211, Lng: -20.0000, Address: "Route 249, Hvolsvöllur",
				Description:  "Hidden in a cleft a few minutes' walk north. Wet feet guaranteed.",
				VisitMinutes: 30, Optional: true, CostCategory: domain.CostActivities,
				Report: &demoReport{Status: domain.StatusSkipped, Story: "Ran out of daylight and patience for wet socks."},
			}},
		}, {
			Title: "Waterfalls and the plane wreck",
			Notes: "The long day. Start early - the wreck is a 45-minute walk each way from the car park.",
			Places: []demoPlace{{
				Name: "Skógafoss", Category: domain.CategoryNature,
				Lat: 63.5321, Lng: -19.5114, Address: "Skógar, 861 Hvolsvöllur",
				URL:         "https://example.com/skogafoss",
				Description: "527 steps to the top. The view back down the valley is the reason.",
				DesiredTime: "09:30", VisitMinutes: 90, CostCategory: domain.CostActivities,
				Report: &demoReport{Status: domain.StatusVisited, Rating: 4, ActualTime: "09:15",
					Story: "Climbed the steps before the buses arrived. Wind at the top took the hat."},
			}, {
				Name: "Sólheimasandur plane wreck", Category: domain.CategoryActivity, Activity: domain.ActivityWalk,
				Difficulty: 2, Lat: 63.4591, Lng: -19.3644, Address: "Route 1, Sólheimasandur",
				Description:  "Flat black sand, no shade, no shelter. Two hours there and back.",
				VisitMinutes: 150, Cost: "12", PerPerson: true, CostCategory: domain.CostActivities,
				Report: &demoReport{Status: domain.StatusVisited, Rating: 3, ActualCost: "12",
					ActualTime: "11:40", ActualEnd: "14:05",
					Story: "A long walk for a photograph everyone already has. The sand is the memorable part."},
			}, {
				Name: "Dyrhólaey", Category: domain.CategorySight,
				Lat: 63.4020, Lng: -19.1264, Address: "Dyrhólaey, 871 Vík",
				Description:  "Puffins on the cliffs in June. The upper road closes in bad weather.",
				VisitMinutes: 60, CostCategory: domain.CostActivities,
				Report: &demoReport{Status: domain.StatusSkipped, Story: "Closed for the evening because of the wind."},
			}},
		}, {
			Title: "Vík and the black beach",
			Notes: "Move to Vík. Short driving day, so there is time to sit still for once.",
			Places: []demoPlace{{
				Name: "Reynisfjara", Category: domain.CategoryNature,
				Lat: 63.4053, Lng: -19.0448, Address: "Reynisfjara, 871 Vík",
				URL:         "https://example.com/reynisfjara",
				Description: "**Do not turn your back on the sea.** The sneaker waves here are lethal.",
				DesiredTime: "11:00", VisitMinutes: 75, CostCategory: domain.CostActivities,
				Report: &demoReport{Status: domain.StatusVisited, Rating: 5, ActualTime: "11:10",
					Story: "Stood well back and watched the water for an hour. The basalt columns do not photograph."},
			}, {
				Name: "Suður-Vík", Category: domain.CategoryFood,
				Lat: 63.4186, Lng: -19.0067, Address: "Suðurvíkurvegur 1, Vík",
				URL: "https://example.com/sudur-vik", BookingRef: "TBL-19:30",
				Description: "Booked for two. The lamb soup is the thing.", DesiredTime: "19:30",
				VisitMinutes: 90, Cost: "45", PerPerson: true, CostCategory: domain.CostFood,
				Report: &demoReport{Status: domain.StatusVisited, Rating: 4, ActualTime: "19:40",
					ActualCost: "52", Story: "The soup lived up to it. Two beers made it dearer than planned."},
			}},
		}, {
			Title: "Back to Keflavík",
			Notes: "Straight back, with one stop. Flight is at 17:40, car back by 15:30.",
			Places: []demoPlace{{
				Name: "Kvernufoss", Category: domain.CategoryNature,
				Lat: 63.5299, Lng: -19.4868, Address: "Skógar, behind the folk museum",
				Description:  "The quiet one. Fifteen minutes' walk from the museum car park.",
				VisitMinutes: 45, Optional: true, CostCategory: domain.CostActivities,
				Report: &demoReport{Status: domain.StatusVisited, Rating: 5, ActualTime: "10:05",
					Story: "Nobody there. The best half hour of the trip, and it was the afterthought."},
			}, {
				Name: "Keflavík airport", Category: domain.CategoryTransport,
				Lat: 63.9850, Lng: -22.6056, Address: "Keflavíkurflugvöllur, 235 Reykjanesbær",
				Description: "Fuel the car at the station before the turn-off; the airport one costs more.",
				DesiredTime: "15:30", VisitMinutes: 60, CostCategory: domain.CostTransport,
				Report: &demoReport{Status: domain.StatusVisited, ActualTime: "15:15", Story: "Scratch was already on the form. No argument."},
			}},
		}},

		Legs: []demoLeg{
			{Day: 1, From: "Keflavík airport", Mode: domain.ModeCar, DistanceM: 128000, DurationS: 6300,
				Cost: "14", Note: "Tunnel toll on the way out of the peninsula."},
			{Day: 2, From: "Skógafoss", Mode: domain.ModeCar, DistanceM: 11000, DurationS: 900},
			{Day: 2, From: "Sólheimasandur plane wreck", Mode: domain.ModeWalk, DistanceM: 7600,
				DurationS: 6000, Note: "The walk out to the wreck and back; no road."},
			{Day: 4, From: "Kvernufoss", Mode: domain.ModeCar, DistanceM: 152000, DurationS: 7800,
				Cost: "22", Note: "Filled the tank at Selfoss."},
		},

		Expenses: []demoExpense{
			{Note: "Car hire, four days", Category: domain.CostTransport, Amount: "290", ActualCost: "290"},
			{Note: "Fuel", Category: domain.CostTransport, Amount: "160", ActualCost: "178"},
			{Note: "Groceries in Hvolsvöllur", Category: domain.CostFood, Amount: "60", Day: 1, ActualCost: "71"},
			{Note: "Wool jumper, regretted nothing", Category: domain.CostShopping, Amount: "90", Day: 3,
				ActualCost: "120"},
		},

		Ideas: []demoPlace{{
			Name: "Fjaðrárgljúfur", Category: domain.CategoryNature,
			Lat: 63.7714, Lng: -18.1725, Description: "Two hours further east than we are going. Next time.",
			VisitMinutes: 90, CostCategory: domain.CostActivities,
		}, {
			Name: "Blue Lagoon", Category: domain.CategoryActivity,
			Lat: 63.8804, Lng: -22.4495, URL: "https://example.com/blue-lagoon",
			Description:  "Only worth it on the way to the airport, and only if booked weeks ahead.",
			VisitMinutes: 180, Cost: "95", PerPerson: true, CostCategory: domain.CostActivities,
		}},

		Report: &demoTripReport{
			Intro: "# Four days on route 1\n\nWe went for the waterfalls and stayed for the wind. " +
				"Everything below is what actually happened, including the two things we did not do.",
			Summary: "## Worth knowing\n\n- The weather decides the itinerary, not the itinerary.\n" +
				"- The quiet waterfall we almost skipped was the best of them.\n" +
				"- Budget more for food than seems reasonable.",
			DayNotes: map[int]string{
				1: "Landed late, queued for the car, and still made the guesthouse in daylight. " +
					"June helps with that.",
				2: "The long day, and it earned the name. Two waterfalls, one very long walk on black sand, " +
					"and a cliff road closed by wind.",
				4: "Left early, stopped once, and were glad we did.",
			},
			Unplanned: &demoUnplanned{
				Day: 3,
				demoPlace: demoPlace{
					Name: "Vík wool shop", Category: domain.CategoryShopping,
					Lat: 63.4179, Lng: -19.0111, Address: "Austurvegur 20, Vík",
					Description:  "Walked past it twice and went in the third time.",
					VisitMinutes: 40, CostCategory: domain.CostShopping,
					Report: &demoReport{Rating: 4, ActualTime: "16:20", ActualCost: "120",
						Story: "Not planned, not regretted. The jumper has been worn ever since."},
				},
			},
			Translation: icelandInRussian(),
		},
	}
}

// icelandInRussian is the Icelandic report in its second language. It is
// content, like every other text of the example, and it leaves some days and
// places untranslated so the fallback to the original can be seen.
func icelandInRussian() *demoTranslation {
	return &demoTranslation{
		Lang:  "ru",
		Title: "Исландия: южное побережье",
		Summary: "Четыре дня по первой трассе, от Рейкьявика до Вика и обратно. Водопады, чёрный песок " +
			"и один очень ветреный день.",
		Intro: "# Четыре дня по первой трассе\n\nЕхали за водопадами, а запомнили ветер. " +
			"Ниже - то, что было на самом деле, включая два места, до которых мы не добрались.",
		Closing: "## Что стоит знать\n\n- Маршрут определяет погода, а не план.\n" +
			"- Тихий водопад, который мы чуть не пропустили, оказался лучшим.\n" +
			"- На еду закладывайте больше, чем кажется разумным.",
		DayTitles: map[int]string{
			1: "Из Рейкьявика в Хвольсвёдлюр",
			2: "Водопады и разбившийся самолёт",
			3: "Вик и чёрный пляж",
		},
		DayNotes: map[int]string{
			1: "Прилетели поздно, отстояли очередь за машиной и всё равно добрались засветло. " +
				"В июне это нетрудно.",
			2: "Самый длинный день, и он это название заслужил. Два водопада, долгая прогулка по чёрному " +
				"песку и дорога на утёс, закрытая из-за ветра.",
		},
		Places: map[string]demoPlaceTranslation{
			"Seljalandsfoss": {
				Name:        "Сельяландсфосс",
				Description: "Тропа проходит *за* водопадом. Нужен дождевик, а не зонт.",
				Story: "Промокли за две минуты, и оно того стоило. Приезжайте рано или поздно: " +
					"в три часа там не протолкнуться.",
			},
			"Skógafoss": {
				Name:  "Скоугафосс",
				Story: "Поднялись по ступеням до автобусов. Наверху ветер унёс шапку.",
			},
			"Reynisfjara": {
				Name:        "Рейнисфьяра",
				Description: "**Не поворачивайтесь к морю спиной.** Внезапные волны здесь смертельно опасны.",
				Story:       "Стояли подальше и час смотрели на воду. Базальтовые столбы на фото не передать.",
			},
			"Kvernufoss": {
				Name:  "Квернюфосс",
				Story: "Ни души. Лучшие полчаса поездки, а ведь заехали туда между делом.",
			},
		},
	}
}

// lisbonTrip is the trip still being planned: no report, and a few open questions
// left in the notes.
func lisbonTrip() demoTrip {
	return demoTrip{
		Title:   "Lisbon in spring",
		Summary: "Three days of tiles, hills and too much coffee. Nothing booked yet except the flights.",
		Start:   "2027-04-16", End: "2027-04-18", Timezone: "Europe/Lisbon",
		Currency: "EUR", Travelers: 3, Budget: "1400",

		Stays: []demoStay{{
			Name: "Alfama rooms", Kind: domain.StayApartment,
			Address: "Rua de São Miguel 44, Lisbon", Lat: 38.7118, Lng: -9.1300,
			CheckIn: "2027-04-16", CheckOut: "2027-04-19", CheckInAt: "14:00", CheckOutAt: "11:00",
			BookingRef: "ALF-77120", URL: "https://example.com/alfama-rooms",
			Contacts: "+351 900 000 111",
			Notes:    "Third floor, no lift. Ask about the luggage room for the last morning.",
			Cost:     "480",
		}},

		Days: []demoDay{{
			Title: "Arrival and Alfama",
			Notes: "Land at 11:20, drop the bags, then nothing but walking. Keep it loose - jet lag is real " +
				"even on a short flight.",
			Places: []demoPlace{{
				Name: "Miradouro das Portas do Sol", Category: domain.CategorySight,
				Lat: 38.7118, Lng: -9.1297, Address: "Largo Portas do Sol, Lisbon",
				Description:  "The postcard view over Alfama. Busy at sunset, empty at nine in the morning.",
				VisitMinutes: 30, CostCategory: domain.CostActivities,
			}, {
				Name: "Sé de Lisboa", Category: domain.CategoryMuseum,
				Lat: 38.7098, Lng: -9.1330, Address: "Largo da Sé, Lisbon",
				URL: "https://example.com/se-lisboa", Description: "Cloister costs extra and is worth it.",
				VisitMinutes: 45, Cost: "5", PerPerson: true, CostCategory: domain.CostActivities,
			}, {
				Name: "Time Out Market", Category: domain.CategoryFood,
				Lat: 38.7067, Lng: -9.1459, Address: "Av. 24 de Julho 49, Lisbon",
				URL:          "https://example.com/time-out-market",
				Description:  "Loud and touristy, and still the easiest first dinner. **Go before seven.**",
				DesiredTime:  "18:30",
				VisitMinutes: 75, Cost: "25", PerPerson: true, CostCategory: domain.CostFood,
			}},
		}, {
			Title: "Belém",
			Notes: "Tram 15 out, walk back along the water if the legs hold. Museum tickets not bought yet - " +
				"check whether they sell out.",
			Places: []demoPlace{{
				Name: "Mosteiro dos Jerónimos", Category: domain.CategoryMuseum,
				Lat: 38.6979, Lng: -9.2065, Address: "Praça do Império, Lisbon",
				URL:         "https://example.com/jeronimos",
				Description: "Queue before ten or book online. The cloister is the point, not the church.",
				DesiredTime: "09:45", VisitMinutes: 90, Cost: "12", PerPerson: true,
				CostCategory: domain.CostActivities,
			}, {
				Name: "Pastéis de Belém", Category: domain.CategoryFood,
				Lat: 38.6975, Lng: -9.2033, Address: "R. de Belém 84, Lisbon",
				Description:  "Eat them standing at the counter, not sitting in the garden.",
				VisitMinutes: 30, Cost: "8", CostCategory: domain.CostFood,
			}, {
				Name: "Padrão dos Descobrimentos", Category: domain.CategorySight,
				Lat: 38.6936, Lng: -9.2058, Address: "Av. Brasília, Lisbon",
				Description:  "The lift to the top is the only reason to pay.",
				VisitMinutes: 45, Optional: true, Cost: "10", PerPerson: true,
				CostCategory: domain.CostActivities,
			}},
		}, {
			Title: "Sintra, or not",
			Notes: "Undecided. Sintra is a day in itself and the trains are grim in the afternoon. " +
				"The alternative is a slow day in Príncipe Real and the botanical garden.",
			Places: []demoPlace{{
				Name: "Palácio da Pena", Category: domain.CategorySight,
				Lat: 38.7876, Lng: -9.3904, Address: "Estrada da Pena, Sintra",
				URL:         "https://example.com/pena",
				Description: "Timed entry only. The park alone is cheaper and half as crowded.",
				DesiredTime: "10:00", VisitMinutes: 180, Cost: "20", PerPerson: true,
				CostCategory: domain.CostActivities,
			}, {
				Name: "Jardim Botânico", Category: domain.CategoryNature,
				Lat: 38.7175, Lng: -9.1478, Address: "R. da Escola Politécnica 58, Lisbon",
				Description:  "The plan B, and honestly the nicer one in April.",
				VisitMinutes: 90, Optional: true, Cost: "6", PerPerson: true,
				CostCategory: domain.CostActivities,
			}},
		}},

		Legs: []demoLeg{
			{Day: 1, From: "Miradouro das Portas do Sol", Mode: domain.ModeWalk, DistanceM: 600, DurationS: 540},
			{Day: 1, From: "Sé de Lisboa", Mode: domain.ModeTransit, DistanceM: 2400, DurationS: 1080,
				Cost: "1.80", Note: "Tram 28 if it is not packed, otherwise the metro."},
			{Day: 2, From: "Alfama rooms", Mode: domain.ModeTransit, DistanceM: 7100, DurationS: 2100,
				Cost: "1.80", Note: "Tram 15 from Praça da Figueira."},
			{Day: 3, From: "Alfama rooms", Mode: domain.ModeTransit, DistanceM: 28000, DurationS: 2700,
				Cost: "4.60", Note: "Train from Rossio. Only if we decide on Sintra."},
		},

		Expenses: []demoExpense{
			{Note: "Flights, three of us", Category: domain.CostTransport, Amount: "420"},
			{Note: "Transport passes", Category: domain.CostTransport, Amount: "40"},
			{Note: "Coffee and pastries, daily", Category: domain.CostFood, Amount: "60"},
			{Note: "Tiles to take home", Category: domain.CostShopping, Amount: "50", Day: 1},
		},

		Ideas: []demoPlace{{
			Name: "LX Factory", Category: domain.CategoryShopping,
			Lat: 38.7026, Lng: -9.1783, Description: "Sunday market. Only works if we stay the weekend.",
			VisitMinutes: 120, CostCategory: domain.CostShopping,
		}, {
			Name: "Fado in a small room", Category: domain.CategoryActivity,
			Lat: 38.7110, Lng: -9.1315,
			Description:  "Ask the flat's owner which one is not a tourist trap. Book on arrival.",
			VisitMinutes: 120, Cost: "35", PerPerson: true, CostCategory: domain.CostActivities,
		}, {
			Name: "Cristo Rei", Category: domain.CategorySight,
			Lat: 38.6784, Lng: -9.1710, Description: "Across the river. The ferry is half the appeal.",
			VisitMinutes: 90, CostCategory: domain.CostActivities,
		}},
	}
}
