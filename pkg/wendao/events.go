package wendao

// 奇遇：每隔两到五小时出现一件，等掌门在两个选项里拿主意，没处理之前不会出现新的。花费与收获随主殿等级增长。

var EventKeys = []string{"wanderer", "raid", "vein", "treasure", "caravan", "insight", "visitor", "pest", "challenge"}

const (
	eventMinGap      = 2 * 60 * 60
	eventMaxGap      = 5 * 60 * 60
	eventInjury      = 4 * 60 * 60
	wandererJoinPct  = 35
	wandererHerb     = 60
	insightHours     = 6
	insightShareHour = 1.5
)

func (s *Sect) spawnEvent(now int64) {
	keys := make([]string, 0, len(EventKeys))
	for _, key := range EventKeys {
		if key == "insight" && s.bestHome(now) == nil {
			continue
		}
		keys = append(keys, key)
	}
	e := &Event{Key: keys[s.Rng.Intn(len(keys))], At: now}
	if e.Key == "insight" {
		var home []*Disciple
		for _, d := range s.Disciples {
			if d.home(now) {
				home = append(home, d)
			}
		}
		e.Target = home[s.Rng.Intn(len(home))].Id
	}
	s.Event = e
}

// eventCost 是奇遇各选项要花的资源，按主殿等级放大。
func (s *Sect) eventCost(key string, choice int) Cost {
	hall := s.Buildings[BuildingHall]
	switch {
	case key == "caravan" && choice == 0:
		return Cost{Stone: 100 * hall}
	case key == "caravan" && choice == 1:
		return Cost{Ore: 80 * hall}
	case key == "visitor" && choice == 0:
		return Cost{Stone: 80 * hall}
	case key == "pest" && choice == 0:
		return Cost{Stone: 60 * hall}
	}
	return Cost{}
}

// eventGain 是奇遇各选项固定的收获(要比斗的选项是赢了之后的收获)，按主殿与相关建筑等级放大。
func (s *Sect) eventGain(key string, choice int) Cost {
	hall := s.Buildings[BuildingHall]
	switch {
	case key == "wanderer" && choice == 0:
		return Cost{Rep: 15}
	case key == "raid" && choice == 0:
		return Cost{Stone: 150 * hall, Rep: 20}
	case key == "vein" && choice == 0:
		return Cost{Ore: 80 * (s.Buildings[BuildingMine] + 1)}
	case key == "vein" && choice == 1:
		return Cost{Stone: 120 * (s.Buildings[BuildingArray] + 1)}
	case key == "caravan" && choice == 0:
		return Cost{Herb: 250 * hall}
	case key == "caravan" && choice == 1:
		return Cost{Stone: 220 * hall}
	case key == "visitor" && choice == 0:
		return Cost{Rep: 12 * hall}
	case key == "visitor" && choice == 1:
		return Cost{Rep: 3}
	case key == "challenge" && choice == 0:
		return Cost{Rep: 10 + 10*hall}
	}
	return Cost{}
}

// fightEvent 表示这个选项要派宗门里最强的弟子去比斗。
func fightEvent(key string, choice int) bool {
	return choice == 0 && (key == "raid" || key == "treasure" || key == "challenge")
}

// EventChoices 返回当前奇遇的两个选项现在能不能选。
func (s *Sect) EventChoices(now int64) [2]bool {
	var choices [2]bool
	if s.Event == nil {
		return choices
	}
	for choice := range choices {
		ok := s.afford(s.eventCost(s.Event.Key, choice))
		if fightEvent(s.Event.Key, choice) && s.bestHome(now) == nil {
			ok = false
		}
		switch {
		case s.Event.Key == "wanderer" && choice == 0:
			ok = s.Pills[PillHeal] > 0 || s.Res.Herb >= wandererHerb
		case s.Event.Key == "insight" && choice == 0:
			target := s.disciple(s.Event.Target)
			ok = target != nil && target.home(now) && !target.Bottleneck()
		}
		choices[choice] = ok
	}
	return choices
}

// EventThreat 是需要比斗的奇遇里对手的战力。
func (s *Sect) EventThreat(key string) int {
	threat := HallPower(s.Buildings[BuildingHall])
	if key == "treasure" {
		threat *= 1.2
	}
	return int(threat + 0.5)
}

func (s *Sect) resolveEvent(choice int, now int64) ([]Ev, error) {
	if s.Event == nil || choice < 0 || choice > 1 {
		return nil, ErrAction
	}
	if !s.EventChoices(now)[choice] {
		return nil, ErrUnavailable
	}
	key := s.Event.Key
	evs := []Ev{{K: "event", S: key, N: choice}}
	cost := s.eventCost(key, choice)
	if err := s.pay(cost); err != nil {
		return nil, err
	}
	evs = append(evs, s.spent(cost)...)
	gain := s.eventGain(key, choice)

	// 比斗：最强的弟子以八到十二成的发挥对上对手。
	var fighter *Disciple
	won := false
	if fightEvent(key, choice) {
		fighter = s.bestHome(now)
		won = float64(s.Power(fighter))*float64(s.Rng.Range(80, 120))/100 >= float64(s.EventThreat(key))
		evs[0].Win = &won
	}
	hurt := func(d *Disciple) {
		d.Injured = max(d.Injured, now) + eventInjury
		evs = append(evs, Ev{K: "injured", Who: &d.Name, N: eventInjury / 3600})
	}

	type pick struct {
		key    string
		choice int
	}
	switch (pick{key, choice}) {
	case pick{"wanderer", 0}:
		if s.Pills[PillHeal] > 0 {
			s.Pills[PillHeal]--
			evs = append(evs, Ev{K: "pill", S: PillHeal, N: -1})
		} else {
			evs = append(evs, s.gain(Cost{Herb: -wandererHerb})...)
		}
		evs = append(evs, s.gain(gain)...)
		if len(s.Disciples) < s.Cap() && s.Rng.Chance(wandererJoinPct) {
			d := s.addDisciple(s.newCandidate(), now)
			evs = append(evs, Ev{K: "joined", Who: &d.Name})
		}
	case pick{"raid", 0}:
		if won {
			evs = append(evs, s.gain(gain)...)
		} else {
			evs = append(evs, s.gain(Cost{Stone: -s.Res.Stone * 15 / 100})...)
			hurt(fighter)
		}
	case pick{"raid", 1}:
		evs = append(evs, s.gain(Cost{Stone: -s.Res.Stone / 10})...)
	case pick{"vein", 0}, pick{"vein", 1}, pick{"caravan", 0}, pick{"caravan", 1}, pick{"visitor", 0}, pick{"visitor", 1}:
		evs = append(evs, s.gain(gain)...)
	case pick{"treasure", 0}:
		if won {
			pill := PillBreak
			if s.Buildings[BuildingHall] >= 5 && s.Rng.Chance(25) {
				pill = PillMarrow
			}
			s.Pills[pill]++
			evs = append(evs, Ev{K: "pill", S: pill, N: 1})
		} else {
			hurt(fighter)
		}
	case pick{"insight", 0}:
		target := s.disciple(s.Event.Target)
		evs = append(evs, s.addExp(target, s.cultivateRate(target, JobCultivate)*insightHours)...)
	case pick{"insight", 1}:
		for _, d := range s.Disciples {
			if d.Mission == 0 {
				evs = append(evs, s.addExp(d, s.cultivateRate(d, JobCultivate)*insightShareHour)...)
			}
		}
	case pick{"pest", 1}:
		evs = append(evs, s.gain(Cost{Herb: -s.Res.Herb * 40 / 100})...)
	case pick{"challenge", 0}:
		if won {
			evs = append(evs, s.gain(gain)...)
		} else {
			evs = append(evs, s.gain(Cost{Rep: -5 * s.Buildings[BuildingHall]})...)
		}
	case pick{"challenge", 1}:
		evs = append(evs, s.gain(Cost{Rep: -3})...)
	}
	s.Event = nil
	s.NextEventAt = now + int64(s.Rng.Range(eventMinGap, eventMaxGap))
	return evs, nil
}

// spent 把已经扣掉的花费写成资源减少的事。
func (s *Sect) spent(c Cost) []Ev {
	var evs []Ev
	for _, item := range []struct {
		key string
		n   int
	}{{"stone", c.Stone}, {"herb", c.Herb}, {"ore", c.Ore}, {"rep", c.Rep}} {
		if item.n > 0 {
			evs = append(evs, Ev{K: "res", S: item.key, N: -item.n})
		}
	}
	return evs
}
