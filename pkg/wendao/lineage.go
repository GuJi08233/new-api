package wendao

import "slices"

const (
	LineageMinRealm = 2
	LineageSlots    = 2
	TeachingBonus   = 15
)

type LineageAction struct {
	Type         string `json:"action"`
	Index        int    `json:"index"`
	Teacher      int    `json:"teacher"`
	CandidateSum string `json:"candidate_sum"`
}

type LineageTeacherView struct {
	Id       int   `json:"id"`
	Students []int `json:"students"`
}

// LineageView 返回金丹以上师长和仍在宗门的亲传弟子。Busy 由控制器补上正在主动出战的弟子，招徒时不得占用。
type LineageView struct {
	MinRealm int                  `json:"min_realm"`
	Slots    int                  `json:"slots"`
	BonusPct int                  `json:"bonus_pct"`
	Teachers []LineageTeacherView `json:"teachers"`
	Busy     []int                `json:"busy"`
}

func (sv *Save) LineageView(now int64) *LineageView {
	if sv.Hero == nil || sv.Sect == nil || !sv.Sect.IsFounded() {
		return nil
	}
	v := &LineageView{MinRealm: LineageMinRealm, Slots: LineageSlots, BonusPct: TeachingBonus,
		Teachers: []LineageTeacherView{}, Busy: []int{}}
	for _, teacher := range sv.Sect.Disciples {
		if teacher.Realm < LineageMinRealm {
			continue
		}
		view := LineageTeacherView{Id: teacher.Id, Students: []int{}}
		for _, student := range sv.Sect.Disciples {
			if student.TeacherId == teacher.Id {
				view.Students = append(view.Students, student.Id)
			}
		}
		v.Teachers = append(v.Teachers, view)
	}
	return v
}

// teachingTeacher 只检查师承与所在地点，伤势的有效时段由自然结算单独计算；主动战斗快照不改变既有的自然修炼。
func (s *Sect) teachingTeacher(student *Disciple) *Disciple {
	if student.TeacherId == 0 || student.Mission != 0 || student.Outpost != 0 {
		return nil
	}
	teacher := s.disciple(student.TeacherId)
	if teacher == nil || teacher.Id == student.Id || teacher.Realm <= student.Realm || teacher.Mission != 0 || teacher.Outpost != 0 {
		return nil
	}
	return teacher
}

func (s *Sect) teachingPct(student *Disciple, now int64) int {
	teacher := s.teachingTeacher(student)
	if teacher == nil || student.Injured > now || teacher.Injured > now || student.Bottleneck() {
		return 0
	}
	return TeachingBonus
}

// LineageApply 从当前候选里收一名亲传，花费与宗门容量沿用普通招募。结算后核对候选摘要，既拒绝旧页，也允许读取副本预览的新候选。
func (sv *Save) LineageApply(a LineageAction, now int64) ([]Ev, error) {
	if a.Type != "recruit" || a.CandidateSum == "" {
		return nil, ErrAction
	}
	s, h := sv.Sect, sv.Hero
	if h == nil || s == nil || !s.IsFounded() || now < s.Last {
		return nil, ErrUnavailable
	}
	teacher := s.disciple(a.Teacher)
	if teacher == nil {
		return nil, ErrAction
	}
	if teacher.Realm < LineageMinRealm || !teacher.home(now) || len(s.Disciples) >= s.Cap() ||
		(h.Battle != nil && slices.Contains(h.Battle.Team, teacher.Id)) {
		return nil, ErrUnavailable
	}
	students := 0
	for _, student := range s.Disciples {
		if student.TeacherId == teacher.Id {
			students++
		}
	}
	if students >= LineageSlots {
		return nil, ErrUnavailable
	}
	evs := s.Tick(now)
	// Tick 可能换新一批候选，摘要必须仍然对应这个格子中的同一个人。
	if a.Index < 0 || a.Index >= len(s.Recruits) || s.Recruits[a.Index] == nil {
		return nil, ErrUnavailable
	}
	sum, err := RequestSum(s.Recruits[a.Index])
	if err != nil {
		return nil, err
	}
	if sum != a.CandidateSum {
		return nil, ErrUnavailable
	}
	more, err := s.recruit(a.Index, now)
	if err != nil {
		return nil, err
	}
	student := s.Disciples[len(s.Disciples)-1]
	teacherName := teacher.Name
	student.TeacherId, student.TeacherName, student.Generation = teacher.Id, &teacherName, max(teacher.Generation, 1)+1
	lineage := Ev{K: "lineage_recruit", Who: &student.Name, V: teacher.Id, N: student.Generation}
	s.record(now, append(more, lineage))
	return append(append(evs, more...), lineage), nil
}
