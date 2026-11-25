package main

import (
	"bytes"
	"embed"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

//go:embed data/*.csv
var embeddedCSV embed.FS

type Leg bool

const (
	Left  Leg = false
	Right Leg = true
)

type Condition string

const (
	ConditionBase   Condition = "Baseline"
	ConditionVerbal Condition = "Verbal-feedback"
	ConditionVR     Condition = "VR-feedback"
)

func (c Condition) Label(lang string) string {
	if lang == "RU" {
		switch c {
		case ConditionBase:
			return "Базовый уровень"
		case ConditionVerbal:
			return "Вербальная обратная связь"
		case ConditionVR:
			return "VR-обратная связь"
		}
	}
	return string(c)
}

type GaitKinematicSnapshot struct {
	ASagP float64
	ASagH float64
	ASagK float64
	ASagA float64
	MSagH float64
	PctGC int
}

type GaitDiscreteParams struct {
	GPS_a          float64
	GVS_aSagP      float64
	GVS_aSagH      float64
	GVS_aSagK      float64
	GVS_aSagA      float64
	aMinSagH       float64
	pctmZeroStSagH float64
}

type GaitCycle struct {
	Snapshots []GaitKinematicSnapshot
	Discrete  GaitDiscreteParams
}

type GaitParams struct {
	Base   []GaitCycle
	Verbal []GaitCycle
	VR     []GaitCycle
}

type Patient struct {
	Subject   string
	Side      string
	Weight    float64
	Height    float64
	LegLength float64
	Params    GaitParams
}

func (p *Patient) ParseLeg() Leg {
	s := strings.ToLower(strings.TrimSpace(p.Side))
	if s == "r" || s == "right" {
		return Right
	}
	return Left
}

func (p *Patient) SideLabel(lang string) string {
	if p.ParseLeg() == Right {
		if lang == "RU" {
			return "Правая"
		}
		return "Right"
	}
	if lang == "RU" {
		return "Левая"
	}
	return "Left"
}

func (p *Patient) HeightCM() float64 {
	if p.Height > 0 && p.Height < 3.0 {
		return p.Height * 100.0
	}
	return p.Height
}

func (p *Patient) LegLengthCM() float64 {
	if p.LegLength > 0 && p.LegLength < 3.0 {
		return p.LegLength * 100.0
	}
	return p.LegLength
}

func (p *Patient) GetCycles(cond Condition) []GaitCycle {
	switch cond {
	case ConditionBase:
		return p.Params.Base
	case ConditionVerbal:
		return p.Params.Verbal
	case ConditionVR:
		return p.Params.VR
	default:
		return nil
	}
}

type CSVType int

const (
	Kinematics CSVType = iota
	Discretes
)

type CSVSource struct {
	Key       string
	Name      string
	Path      string
	Condition Condition
	Type      CSVType
	Raw       []byte
	Custom    bool
}

func defaultCSVSources() ([]CSVSource, error) {
	defs := []CSVSource{
		{Key: "disc_base", Name: "Discrete_Parameters_Baseline.csv", Path: "data/Discrete_Parameters_Baseline.csv", Condition: ConditionBase, Type: Discretes},
		{Key: "disc_verb", Name: "Discrete_Parameters_Verbal.csv", Path: "data/Discrete_Parameters_Verbal.csv", Condition: ConditionVerbal, Type: Discretes},
		{Key: "disc_vr", Name: "Discrete_Parameters_VR.csv", Path: "data/Discrete_Parameters_VR.csv", Condition: ConditionVR, Type: Discretes},
		{Key: "kin_base", Name: "Kinematics_Kinetics_Baseline.csv", Path: "data/Kinematics_Kinetics_Baseline.csv", Condition: ConditionBase, Type: Kinematics},
		{Key: "kin_verb", Name: "Kinematics_Kinetics_Verbal.csv", Path: "data/Kinematics_Kinetics_Verbal.csv", Condition: ConditionVerbal, Type: Kinematics},
		{Key: "kin_vr", Name: "Kinematics_Kinetics_VR.csv", Path: "data/Kinematics_Kinetics_VR.csv", Condition: ConditionVR, Type: Kinematics},
	}
	for i := range defs {
		b, err := embeddedCSV.ReadFile(defs[i].Path)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", defs[i].Path, err)
		}
		defs[i].Raw = b
	}
	return defs, nil
}

func parseCSVTable(raw []byte) ([][]string, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	r.Comma = ';'
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 && len(rows[0]) <= 1 {
		r2 := csv.NewReader(bytes.NewReader(raw))
		r2.Comma = ','
		if rows2, err2 := r2.ReadAll(); err2 == nil && len(rows2) > 0 && len(rows2[0]) > 1 {
			return rows2, nil
		}
	}
	return rows, nil
}

type patientBuilder struct {
	patient     *Patient
	kinCursors  map[Condition]int
	discCursors map[Condition]int
}

func (b *patientBuilder) cyclesPtr(cond Condition) *[]GaitCycle {
	switch cond {
	case ConditionBase:
		return &b.patient.Params.Base
	case ConditionVerbal:
		return &b.patient.Params.Verbal
	case ConditionVR:
		return &b.patient.Params.VR
	default:
		return nil
	}
}

func (b *patientBuilder) addSnapshots(cond Condition, snaps []GaitKinematicSnapshot) {
	cycles := b.cyclesPtr(cond)
	idx := b.kinCursors[cond]
	for len(*cycles) <= idx {
		*cycles = append(*cycles, GaitCycle{})
	}
	(*cycles)[idx].Snapshots = snaps
	b.kinCursors[cond] = idx + 1
}

func (b *patientBuilder) addDiscrete(cond Condition, disc GaitDiscreteParams) {
	cycles := b.cyclesPtr(cond)
	idx := b.discCursors[cond]
	for len(*cycles) <= idx {
		*cycles = append(*cycles, GaitCycle{})
	}
	(*cycles)[idx].Discrete = disc
	b.discCursors[cond] = idx + 1
}
func loadAllPatients(sources []CSVSource) ([]*Patient, error) {
	builders := make(map[string]*patientBuilder)
	var order []string

	for _, src := range sources {
		if len(src.Raw) == 0 {
			continue
		}
		reader := csv.NewReader(bytes.NewReader(src.Raw))
		reader.Comma = ';'

		headers, err := reader.Read()
		if err != nil {
			return nil, fmt.Errorf("header %s: %w", src.Name, err)
		}
		if len(headers) <= 1 {
			reader = csv.NewReader(bytes.NewReader(src.Raw))
			reader.Comma = ','
			headers, err = reader.Read()
			if err != nil {
				return nil, fmt.Errorf("header %s: %w", src.Name, err)
			}
		}

		var plan []kinematicStepPlan
		if src.Type == Kinematics {
			plan = buildKinematicPlan(headers)
		}

		for {
			rec, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("row %s: %w", src.Name, err)
			}

			subj := safeStr(rec, 0)
			if subj == "" {
				continue
			}
			b, ok := builders[subj]
			if !ok {
				b = &patientBuilder{
					patient: &Patient{
						Subject:   subj,
						Side:      safeStr(rec, 1),
						Weight:    safeFloat(rec, 2),
						Height:    safeFloat(rec, 3),
						LegLength: safeFloat(rec, 4),
					},
					kinCursors:  make(map[Condition]int),
					discCursors: make(map[Condition]int),
				}
				builders[subj] = b
				order = append(order, subj)
			}

			if src.Type == Kinematics {
				snaps := make([]GaitKinematicSnapshot, len(plan))
				for j, step := range plan {
					snaps[j] = GaitKinematicSnapshot{
						PctGC: step.pctGC,
						ASagP: safeFloat(rec, step.aSagP),
						ASagH: safeFloat(rec, step.aSagH),
						ASagK: safeFloat(rec, step.aSagK),
						ASagA: safeFloat(rec, step.aSagA),
						MSagH: safeFloat(rec, step.mSagH),
					}
				}
				b.addSnapshots(src.Condition, snaps)
			} else {
				b.addDiscrete(src.Condition, GaitDiscreteParams{
					GPS_a:          safeFloat(rec, 5),
					GVS_aSagP:      safeFloat(rec, 6),
					GVS_aSagH:      safeFloat(rec, 7),
					GVS_aSagK:      safeFloat(rec, 8),
					GVS_aSagA:      safeFloat(rec, 9),
					aMinSagH:       safeFloat(rec, 10),
					pctmZeroStSagH: safeFloat(rec, 11),
				})
			}
		}
	}

	patients := make([]*Patient, 0, len(order))
	for _, subj := range order {
		patients = append(patients, builders[subj].patient)
	}
	return patients, nil
}

type kinematicStepPlan struct {
	pctGC, aSagP, aSagH, aSagK, aSagA, mSagH int
}

func buildKinematicPlan(headers []string) []kinematicStepPlan {
	stepMap := make(map[int]*kinematicStepPlan)
	for i := 5; i < len(headers); i++ {
		param, pctStr, ok := strings.Cut(strings.TrimSpace(headers[i]), "_pct_GC_")
		if !ok {
			continue
		}
		pct, err := strconv.Atoi(pctStr)
		if err != nil {
			continue
		}
		st, exists := stepMap[pct]
		if !exists {
			st = &kinematicStepPlan{pctGC: pct, aSagP: -1, aSagH: -1, aSagK: -1, aSagA: -1, mSagH: -1}
			stepMap[pct] = st
		}
		switch param {
		case "aSagP":
			st.aSagP = i
		case "aSagH":
			st.aSagH = i
		case "aSagK":
			st.aSagK = i
		case "aSagA":
			st.aSagA = i
		case "mSagH":
			st.mSagH = i
		}
	}
	steps := make([]kinematicStepPlan, 0, len(stepMap))
	for _, st := range stepMap {
		steps = append(steps, *st)
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].pctGC < steps[j].pctGC })
	return steps
}

func safeStr(rec []string, idx int) string {
	if idx < 0 || idx >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[idx])
}

func safeFloat(rec []string, idx int) float64 {
	s := safeStr(rec, idx)
	if s == "" || strings.EqualFold(s, "nan") || strings.EqualFold(s, "na") {
		return math.NaN()
	}
	s = strings.ReplaceAll(s, ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

func (p *Patient) DisplayName(lang string) string {
	if _, numStr, found := strings.Cut(p.Subject, "_"); found {
		if lang == "RU" {
			return fmt.Sprintf("Пациент №%s", numStr)
		}
		return fmt.Sprintf("Patient №%s", numStr)
	} else {
		return p.Subject
	}
}
