package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
)

const (
	clsBase   = 0
	clsVerbal = 1
	clsVR     = 2
	numCls    = 3
)

type CycleEval struct {
	CycleIndex int
	Score      float64
	IsBad      bool
	Reasons    []string
}

type CondDelta struct {
	Condition Condition
	Cycles    int
	BadCycles int
	BaseGPS   float64
	CondGPS   float64
	DeltaGPS  float64
	PctChange float64
	CohensD   float64
}

type PatientAnalysis struct {
	Subject     string
	StatusEN    string
	StatusRU    string
	StatusKind  int // 0: neutral, 1: improved, -1: worsened, 2: unstable steps
	CycleEvals  map[Condition][]CycleEval
	Comparisons []CondDelta
}

func AnalyzePatient(p *Patient, zThresh float64) PatientAnalysis {
	if zThresh <= 0 {
		zThresh = 2.5
	}
	res := PatientAnalysis{
		Subject:    p.Subject,
		CycleEvals: make(map[Condition][]CycleEval),
	}

	cleanGPS := make(map[Condition][]float64)
	for _, cond := range []Condition{ConditionBase, ConditionVerbal, ConditionVR} {
		cycles := p.GetCycles(cond)
		evals := detectBadSteps(cycles, zThresh)
		res.CycleEvals[cond] = evals

		for i, c := range cycles {
			if !evals[i].IsBad && !math.IsNaN(c.Discrete.GPS_a) {
				cleanGPS[cond] = append(cleanGPS[cond], c.Discrete.GPS_a)
			}
		}
		if len(cleanGPS[cond]) < 2 {
			cleanGPS[cond] = nil
			for _, c := range cycles {
				if !math.IsNaN(c.Discrete.GPS_a) {
					cleanGPS[cond] = append(cleanGPS[cond], c.Discrete.GPS_a)
				}
			}
		}
	}

	baseVals := cleanGPS[ConditionBase]
	baseMean := mean(baseVals)
	bestStatusEN, bestStatusRU := "Stable (No Shift)", "Стабильно (без сдвига)"

	totalBad := 0
	for _, evs := range res.CycleEvals {
		for _, e := range evs {
			if e.IsBad {
				totalBad++
			}
		}
	}
	if totalBad > 0 {
		res.StatusKind = 2
		bestStatusEN = fmt.Sprintf("No Shift (%d Unstable Steps)", totalBad)
		bestStatusRU = fmt.Sprintf("Без сдвига (нестаб. шагов: %d)", totalBad)
	}

	bestAbsD := 0.0
	for _, cond := range []Condition{ConditionVerbal, ConditionVR} {
		cycles := p.GetCycles(cond)
		if len(cycles) == 0 || len(baseVals) == 0 {
			continue
		}
		badCnt := 0
		for _, e := range res.CycleEvals[cond] {
			if e.IsBad {
				badCnt++
			}
		}
		cMean := mean(cleanGPS[cond])
		delta := cMean - baseMean
		pct := 0.0
		if baseMean != 0 {
			pct = (delta / baseMean) * 100.0
		}
		d := cohensD(baseVals, cleanGPS[cond])

		res.Comparisons = append(res.Comparisons, CondDelta{
			Condition: cond,
			Cycles:    len(cycles),
			BadCycles: badCnt,
			BaseGPS:   baseMean,
			CondGPS:   cMean,
			DeltaGPS:  delta,
			PctChange: pct,
			CohensD:   d,
		})

		if math.Abs(d) > bestAbsD {
			if d >= 0.4 && pct <= -3.0 {
				bestAbsD = math.Abs(d)
				res.StatusKind = 1
				bestStatusEN = fmt.Sprintf("%s: Improved (%.1f%%)", cond.Label("EN"), pct)
				bestStatusRU = fmt.Sprintf("%s: улучшение (%.1f%%)", cond.Label("RU"), pct)
			} else if d <= -0.4 && pct >= 3.0 {
				bestAbsD = math.Abs(d)
				res.StatusKind = -1
				bestStatusEN = fmt.Sprintf("%s: Worsened (+%.1f%%)", cond.Label("EN"), pct)
				bestStatusRU = fmt.Sprintf("%s: ухудшение (+%.1f%%)", cond.Label("RU"), pct)
			}
		}
	}

	res.StatusEN = bestStatusEN
	res.StatusRU = bestStatusRU
	return res
}

func detectBadSteps(cycles []GaitCycle, zThresh float64) []CycleEval {
	n := len(cycles)
	evals := make([]CycleEval, n)
	for i := range evals {
		evals[i] = CycleEval{CycleIndex: i + 1}
	}
	if n < 3 {
		return evals
	}

	fGPS := make([]float64, n)
	fKneeGVS := make([]float64, n)
	fAnkleGVS := make([]float64, n)
	fKneeROM := make([]float64, n)
	fJerk := make([]float64, n)

	for i, c := range cycles {
		fGPS[i] = c.Discrete.GPS_a
		fKneeGVS[i] = c.Discrete.GVS_aSagK
		fAnkleGVS[i] = c.Discrete.GVS_aSagA
		fKneeROM[i] = snapROM(c.Snapshots, func(s GaitKinematicSnapshot) float64 { return s.ASagK })
		fJerk[i] = kneeJerk(c.Snapshots)
	}

	feats := []struct {
		name string
		vals []float64
	}{
		{"GPS", fGPS},
		{"Knee GVS", fKneeGVS},
		{"Ankle GVS", fAnkleGVS},
		{"Knee ROM", fKneeROM},
		{"Jerk", fJerk},
	}

	for _, f := range feats {
		med := median(f.vals)
		if math.IsNaN(med) {
			continue
		}
		var devs []float64
		for _, v := range f.vals {
			if !math.IsNaN(v) {
				devs = append(devs, math.Abs(v-med))
			}
		}
		mad := median(devs)
		if mad < 1e-6 || math.IsNaN(mad) {
			continue
		}
		for i, v := range f.vals {
			if math.IsNaN(v) {
				continue
			}
			rz := 0.6745 * math.Abs(v-med) / mad
			if rz > evals[i].Score {
				evals[i].Score = rz
			}
			if rz > zThresh {
				evals[i].IsBad = true
				evals[i].Reasons = append(evals[i].Reasons, fmt.Sprintf("%s %.1fσ", f.name, rz))
			}
		}
	}
	return evals
}

func snapROM(snaps []GaitKinematicSnapshot, get func(GaitKinematicSnapshot) float64) float64 {
	minV, maxV := math.Inf(1), math.Inf(-1)
	for _, s := range snaps {
		v := get(s)
		if !math.IsNaN(v) {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
	}
	if math.IsInf(minV, 1) {
		return math.NaN()
	}
	return maxV - minV
}

func kneeJerk(snaps []GaitKinematicSnapshot) float64 {
	if len(snaps) < 3 {
		return 0
	}
	sum, cnt := 0.0, 0
	for i := 1; i < len(snaps)-1; i++ {
		a, b, c := snaps[i-1].ASagK, snaps[i].ASagK, snaps[i+1].ASagK
		if !math.IsNaN(a) && !math.IsNaN(b) && !math.IsNaN(c) {
			d := c - 2*b + a
			sum += d * d
			cnt++
		}
	}
	if cnt == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(cnt))
}

func humanFeatureName(code, lang string) string {
	type pair struct{ en, ru string }
	labels := map[string]pair{
		"GPS_a":          {"Gait profile score (GPS)", "Индекс профиля походки (GPS)"},
		"GVS_aSagP":      {"Pelvis tilt deviation (GVS)", "Отклонение наклона таза (GVS)"},
		"GVS_aSagH":      {"Hip angle deviation (GVS)", "Отклонение угла бедра (GVS)"},
		"GVS_aSagK":      {"Knee angle deviation (GVS)", "Отклонение угла колена (GVS)"},
		"GVS_aSagA":      {"Ankle angle deviation (GVS)", "Отклонение голеностопа (GVS)"},
		"aMinSagH":       {"Minimal hip extension angle", "Мин. угол разгибания бедра"},
		"pctmZeroStSagH": {"Hip moment zero-crossing point(% Step)", "Точка перехода момента тазобедренного сустава через ноль (% цикла шага)"},
		"Knee_ROM":       {"Knee range of motion (ROM)", "Амплитуда движения колена (ROM)"},
		"Hip_ROM":        {"Hip range of motion (ROM)", "Амплитуда движения бедра (ROM)"},
		"Ankle_ROM":      {"Ankle range of motion (ROM)", "Амплитуда голеностопа (ROM)"},
	}
	if item, ok := labels[code]; ok {
		if lang == "RU" {
			return item.ru
		}
		return item.en
	}
	return code
}

func GenerateClinicalSummary(p *Patient, a PatientAnalysis, lang string) string {
	var b strings.Builder
	if strings.EqualFold(strings.TrimSpace(lang), "RU") {
		fmt.Fprintf(&b, "КЛИНИЧЕСКАЯ СВОДКА: %s (Сторона: %s, Рост: %.0f см, Вес: %.1f кг)\n",
			p.DisplayName("RU"), p.SideLabel("RU"), p.HeightCM(), p.Weight)
		fmt.Fprintf(&b, "Учтённые шаги - %s: %d | %s: %d | %s: %d\n\n",
			ConditionBase.Label("RU"), len(p.Params.Base),
			ConditionVerbal.Label("RU"), len(p.Params.Verbal),
			ConditionVR.Label("RU"), len(p.Params.VR))

		b.WriteString("1. Анализ стабильности шагов (Robust Z-Score / MAD):\n")
		for _, cond := range []Condition{ConditionBase, ConditionVerbal, ConditionVR} {
			var bad []string
			for _, ev := range a.CycleEvals[cond] {
				if ev.IsBad {
					bad = append(bad, fmt.Sprintf("Шаг %d (%s)", ev.CycleIndex, strings.Join(ev.Reasons, ", ")))
				}
			}
			if len(bad) == 0 {
				fmt.Fprintf(&b, "  • %s: все шаги стабильны.\n", cond.Label("RU"))
			} else {
				fmt.Fprintf(&b, "  • %s: выявлены нестабильные шаги - %s.\n", cond.Label("RU"), strings.Join(bad, "; "))
			}
		}

		b.WriteString("\n2. Сравнение условий относительно базового (по индексу GPS):\n")
		for _, cmp := range a.Comparisons {
			trend := "без значимых изменений"
			if cmp.CohensD >= 0.4 && cmp.PctChange <= -3.0 {
				trend = "улучшение симметрии и паттерна ходьбы"
			} else if cmp.CohensD <= -0.4 && cmp.PctChange >= 3.0 {
				trend = "увеличение отклонения от нормы"
			}
			fmt.Fprintf(&b, "  • %s: GPS %.2f° → %.2f° (Δ %+.2f° / %+.1f%%, Cohen's d = %.2f) - %s.\n",
				cmp.Condition.Label("RU"), cmp.BaseGPS, cmp.CondGPS, cmp.DeltaGPS, cmp.PctChange, cmp.CohensD, trend)
		}

		b.WriteString("\n3. Справочная связь с категориями МКФ (ICF):\n")
		b.WriteString("  • b770 (Функции стереотипа походки): отражает кинематические отклонения таза, бедра, колена и голеностопа (GPS/GVS) и вариабельность шагов.\n")
		b.WriteString("  • d450 (Ходьба): оценивает функциональную адаптацию ходьбы при вербальной и VR-обратной связи.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "CLINICAL GAIT SUMMARY: %s (Side: %s, Height: %.0f cm, Weight: %.1f kg)\n",
		p.DisplayName("EN"), p.SideLabel("EN"), p.HeightCM(), p.Weight)
	fmt.Fprintf(&b, "Analyzed Steps - %s: %d | %s: %d | %s: %d\n\n",
		ConditionBase.Label("EN"), len(p.Params.Base),
		ConditionVerbal.Label("EN"), len(p.Params.Verbal),
		ConditionVR.Label("EN"), len(p.Params.VR))

	b.WriteString("1. Step Stability & Outlier Detection (Robust Z-Score / MAD):\n")
	for _, cond := range []Condition{ConditionBase, ConditionVerbal, ConditionVR} {
		var bad []string
		for _, ev := range a.CycleEvals[cond] {
			if ev.IsBad {
				bad = append(bad, fmt.Sprintf("Step %d (%s)", ev.CycleIndex, strings.Join(ev.Reasons, ", ")))
			}
		}
		if len(bad) == 0 {
			fmt.Fprintf(&b, "  • %s: all recorded steps are consistent.\n", cond.Label("EN"))
		} else {
			fmt.Fprintf(&b, "  • %s: unstable step(s) flagged - %s.\n", cond.Label("EN"), strings.Join(bad, "; "))
		}
	}

	b.WriteString("\n2. Condition Shifts vs. Baseline (Gait Profile Score):\n")
	for _, cmp := range a.Comparisons {
		trend := "no meaningful shift"
		if cmp.CohensD >= 0.4 && cmp.PctChange <= -3.0 {
			trend = "improvement toward normative gait"
		} else if cmp.CohensD <= -0.4 && cmp.PctChange >= 3.0 {
			trend = "increased gait deviation"
		}
		fmt.Fprintf(&b, "  • %s: GPS %.2f° → %.2f° (Δ %+.2f° / %+.1f%%, Cohen's d = %.2f) - %s.\n",
			cmp.Condition.Label("EN"), cmp.BaseGPS, cmp.CondGPS, cmp.DeltaGPS, cmp.PctChange, cmp.CohensD, trend)
	}

	b.WriteString("\n3. ICF Reference Mapping:\n")
	b.WriteString("  • b770 (Gait pattern functions): sagittal joint deviation indices (GPS/GVS) and step-to-step stability.\n")
	b.WriteString("  • d450 (Walking): functional locomotor responsiveness to Verbal and VR biofeedback cues.\n")

	return b.String()
}

func GetClinicalGlossary(lang string) string {
	if strings.EqualFold(strings.TrimSpace(lang), "RU") {
		return "СПРАВОЧНИК ТЕРМИНОВ (АННОТАЦИЯ):\n" +
			"• GPS (Индекс профиля походки) — общий показатель отклонения кинематики походки от нормы (в градусах). Чем ниже значение, тем ближе походка к норме (норма ~4.5–6.2°).\n" +
			"• GVS (Индекс вариабельности походки) — отклонение от нормы для конкретного сустава (таз, бедро, колено, голеностоп) за весь цикл шага.\n" +
			"• Knee / Hip / Ankle ROM (Амплитуда движений) — разница между максимальным сгибанием и разгибанием сустава за один шаг. Снижение ROM указывает на скованность или щадящий режим ходьбы.\n" +
			"• Jerk (Рывок / плавность движения) — резкость изменения углового ускорения колена. Высокий Jerk означает дёрганое, нестабильное или плохо контролируемое движение.\n" +
			"• MAD Z-Score (σ) — устойчивый к выбросам показатель отклонения шага от медианы пациента. Шаги выше порога помечаются как нестабильные.\n" +
			"• Cohen's d (Размер эффекта) — сила сдвига между базовым уровнем и обратной связью (≥ 0.4 — клинически заметное улучшение)."
	}
	return "CLINICAL GLOSSARY & METRIC GUIDE:\n" +
		"• GPS (Gait Profile Score) — overall kinematic deviation from normative gait (in degrees). Lower values indicate a more normal gait pattern (healthy norm ~4.5–6.2°).\n" +
		"• GVS (Gait Variable Score) — root-mean-square deviation from normal for a single joint (Pelvis, Hip, Knee, or Ankle) across the gait cycle.\n" +
		"• Knee / Hip / Ankle ROM (Range of Motion) — total angular excursion (max flexion minus max extension) during a single step. Reduced ROM suggests stiffness or guarding.\n" +
		"• Jerk (Movement Smoothness) — rate of change of knee angular acceleration. High Jerk indicates abrupt, jerky, or poorly controlled limb motion.\n" +
		"• MAD Z-Score (σ) — median-based outlier score measuring how far a step deviates from the patient's typical step. Steps above the threshold are flagged as unstable.\n" +
		"• Cohen's d (Effect Size) — standardized magnitude of change from Baseline to feedback (≥ 0.4 indicates a meaningful shift)."
}

type mlSample struct {
	Subject  string
	Label    int
	Features []float64
}

var mlFeatNames = []string{
	"GPS_a", "GVS_aSagP", "GVS_aSagH", "GVS_aSagK", "GVS_aSagA",
	"aMinSagH", "pctmZeroStSagH", "Knee_ROM", "Hip_ROM", "Ankle_ROM",
}

type FeatureRank struct {
	Name       string
	Importance float64
}

type SubjectMLMetrics struct {
	Subjects    int
	Cycles      int
	Accuracy    float64
	MacroF1     float64
	ClassF1     []float64
	Confusion   [][]int
	TopFeatures []FeatureRank
}

func EvaluateSubjectSplitML(patients []*Patient) SubjectMLMetrics {
	var samples []mlSample
	conds := []struct {
		c Condition
		l int
	}{
		{ConditionBase, clsBase},
		{ConditionVerbal, clsVerbal},
		{ConditionVR, clsVR},
	}

	subjSet := make(map[string]bool)
	for _, p := range patients {
		bMean, bSD := baselineNormStats(p.GetCycles(ConditionBase))
		for _, cm := range conds {
			for _, cyc := range p.GetCycles(cm.c) {
				raw := cycleFeatVec(cyc)
				norm := make([]float64, len(raw))
				for j := range raw {
					v := raw[j]
					if math.IsNaN(v) {
						v = bMean[j]
					}
					if math.IsNaN(v) {
						v = 0
					}
					if bSD[j] > 1e-5 {
						norm[j] = (v - bMean[j]) / bSD[j]
					} else {
						norm[j] = v - bMean[j]
					}
				}
				samples = append(samples, mlSample{Subject: p.Subject, Label: cm.l, Features: norm})
				subjSet[p.Subject] = true
			}
		}
	}

	conf := make([][]int, numCls)
	for r := 0; r < numCls; r++ {
		conf[r] = make([]int, numCls)
	}
	m := SubjectMLMetrics{
		Subjects:  len(subjSet),
		Cycles:    len(samples),
		ClassF1:   make([]float64, numCls),
		Confusion: conf,
	}
	if len(subjSet) < 2 || len(samples) == 0 {
		return m
	}

	gain := make([]float64, len(mlFeatNames))
	rng := rand.New(rand.NewSource(42))

	for testSubj := range subjSet {
		var train, test []mlSample
		for _, s := range samples {
			if s.Subject == testSubj {
				test = append(test, s)
			} else {
				train = append(train, s)
			}
		}
		if len(train) == 0 || len(test) == 0 {
			continue
		}
		forest := trainForest(train, 30, 5, gain, rng)
		for _, ts := range test {
			pred := forest.predict(ts.Features)
			m.Confusion[ts.Label][pred]++
		}
	}

	correct := 0
	for c := 0; c < numCls; c++ {
		correct += m.Confusion[c][c]
	}
	m.Accuracy = float64(correct) / float64(len(samples)) * 100.0

	f1Sum, valid := 0.0, 0
	for c := 0; c < numCls; c++ {
		tp := float64(m.Confusion[c][c])
		fp, fn := 0.0, 0.0
		for k := 0; k < numCls; k++ {
			if k != c {
				fp += float64(m.Confusion[k][c])
				fn += float64(m.Confusion[c][k])
			}
		}
		if denom := 2*tp + fp + fn; denom > 0 {
			f1 := (2 * tp) / denom
			m.ClassF1[c] = f1 * 100.0
			f1Sum += f1
			valid++
		}
	}
	if valid > 0 {
		m.MacroF1 = (f1Sum / float64(valid)) * 100.0
	}

	gSum := 0.0
	for _, g := range gain {
		gSum += g
	}
	for i, name := range mlFeatNames {
		imp := 0.0
		if gSum > 0 {
			imp = gain[i] / gSum * 100.0
		}
		m.TopFeatures = append(m.TopFeatures, FeatureRank{Name: name, Importance: imp})
	}
	sort.Slice(m.TopFeatures, func(i, j int) bool { return m.TopFeatures[i].Importance > m.TopFeatures[j].Importance })
	return m
}

func cycleFeatVec(c GaitCycle) []float64 {
	return []float64{
		c.Discrete.GPS_a,
		c.Discrete.GVS_aSagP,
		c.Discrete.GVS_aSagH,
		c.Discrete.GVS_aSagK,
		c.Discrete.GVS_aSagA,
		c.Discrete.aMinSagH,
		c.Discrete.pctmZeroStSagH,
		snapROM(c.Snapshots, func(s GaitKinematicSnapshot) float64 { return s.ASagK }),
		snapROM(c.Snapshots, func(s GaitKinematicSnapshot) float64 { return s.ASagH }),
		snapROM(c.Snapshots, func(s GaitKinematicSnapshot) float64 { return s.ASagA }),
	}
}

func baselineNormStats(base []GaitCycle) ([]float64, []float64) {
	dim := len(mlFeatNames)
	means, sds := make([]float64, dim), make([]float64, dim)
	if len(base) == 0 {
		return means, sds
	}
	mat := make([][]float64, len(base))
	for i, c := range base {
		mat[i] = cycleFeatVec(c)
	}
	for j := 0; j < dim; j++ {
		var col []float64
		for i := range base {
			if !math.IsNaN(mat[i][j]) {
				col = append(col, mat[i][j])
			}
		}
		means[j] = mean(col)
		sds[j] = math.Sqrt(variance(col))
	}
	return means, sds
}

type rfNode struct {
	feat   int
	thresh float64
	left   *rfNode
	right  *rfNode
	probs  []float64
	leaf   bool
}

type rfModel struct{ trees []*rfNode }

func trainForest(data []mlSample, nTrees, maxDepth int, gain []float64, rng *rand.Rand) rfModel {
	m := rfModel{trees: make([]*rfNode, nTrees)}
	for t := 0; t < nTrees; t++ {
		boot := make([]mlSample, len(data))
		for i := range boot {
			boot[i] = data[rng.Intn(len(data))]
		}
		m.trees[t] = buildRFNode(boot, 0, maxDepth, gain, rng)
	}
	return m
}

func (m rfModel) predict(x []float64) int {
	votes := make([]float64, numCls)
	for _, tr := range m.trees {
		p := tr.eval(x)
		for c := 0; c < numCls; c++ {
			votes[c] += p[c]
		}
	}
	best := clsBase
	for c := 1; c < numCls; c++ {
		if votes[c] > votes[best] {
			best = c
		}
	}
	return best
}

func (n *rfNode) eval(x []float64) []float64 {
	if n.leaf {
		return n.probs
	}
	if x[n.feat] <= n.thresh {
		return n.left.eval(x)
	}
	return n.right.eval(x)
}

func gini(s []mlSample) float64 {
	if len(s) == 0 {
		return 0
	}
	cnt := make([]float64, numCls)
	for _, item := range s {
		cnt[item.Label]++
	}
	n := float64(len(s))
	sumSq := 0.0
	for c := 0; c < numCls; c++ {
		p := cnt[c] / n
		sumSq += p * p
	}
	return 1.0 - sumSq
}

func buildRFNode(s []mlSample, depth, maxDepth int, gain []float64, rng *rand.Rand) *rfNode {
	nd := &rfNode{probs: make([]float64, numCls)}
	cnt := make([]float64, numCls)
	for _, item := range s {
		cnt[item.Label]++
	}
	if len(s) > 0 {
		for c := 0; c < numCls; c++ {
			nd.probs[c] = cnt[c] / float64(len(s))
		}
	}
	pGini := gini(s)
	if depth >= maxDepth || len(s) < 4 || pGini < 1e-4 {
		nd.leaf = true
		return nd
	}

	perm := rng.Perm(len(mlFeatNames))
	subFeat := perm[:3]
	step := 1
	if len(s) > 25 {
		step = len(s) / 25
	}

	bestGain, bestF, bestT := 0.0, -1, 0.0
	var bestL, bestR []mlSample

	for _, f := range subFeat {
		for idx := 0; idx < len(s); idx += step {
			th := s[idx].Features[f]
			var l, r []mlSample
			for _, item := range s {
				if item.Features[f] <= th {
					l = append(l, item)
				} else {
					r = append(r, item)
				}
			}
			if len(l) < 2 || len(r) < 2 {
				continue
			}
			g := pGini - (float64(len(l))/float64(len(s))*gini(l) + float64(len(r))/float64(len(s))*gini(r))
			if g > bestGain {
				bestGain, bestF, bestT, bestL, bestR = g, f, th, l, r
			}
		}
	}
	if bestF == -1 {
		nd.leaf = true
		return nd
	}
	gain[bestF] += bestGain * float64(len(s))
	nd.feat, nd.thresh = bestF, bestT
	nd.left = buildRFNode(bestL, depth+1, maxDepth, gain, rng)
	nd.right = buildRFNode(bestR, depth+1, maxDepth, gain, rng)
	return nd
}

func BuildPatientReportHTML(p *Patient, a PatientAnalysis, summary, lang string) string {
	var rows strings.Builder
	for _, cmp := range a.Comparisons {
		fmt.Fprintf(&rows, "<tr><td>%s</td><td>%d (%d unstable)</td><td>%.2f°</td><td>%.2f°</td><td>%+.2f° (%+.1f%%)</td><td>%.2f</td></tr>",
			cmp.Condition.Label(lang), cmp.Cycles, cmp.BadCycles, cmp.BaseGPS, cmp.CondGPS, cmp.DeltaGPS, cmp.PctChange, cmp.CohensD)
	}
	return fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Gait Report - %s</title>
<style>body{font-family:sans-serif;max-width:860px;margin:24px auto;line-height:1.5;color:#222}
table{border-collapse:collapse;width:100%%;margin:16px 0}th,td{border:1px solid #ccc;padding:8px;text-align:left}
th{background:#f2f4f8}pre{background:#f8f9fa;padding:12px;border-radius:6px;white-space:pre-wrap}</style></head>
<body><h1>Clinical Gait Report: %s</h1>
<p><b>Side:</b> %s | <b>Height:</b> %.0f cm | <b>Weight:</b> %.1f kg | <b>Leg Length:</b> %.1f cm</p>
<h2>Condition Comparison vs. Baseline</h2>
<table><tr><th>Condition</th><th>Steps</th><th>Baseline GPS</th><th>Condition GPS</th><th>Change</th><th>Cohen's d</th></tr>%s</table>
<h2>AI Summary & ICF Reference (b770 / d450)</h2><pre>%s</pre></body></html>`,
		p.DisplayName(lang), p.DisplayName(lang), p.SideLabel(lang), p.HeightCM(), p.Weight, p.LegLengthCM(), rows.String(), summary)
}

func mean(vals []float64) float64 {
	s, n := 0.0, 0
	for _, v := range vals {
		if !math.IsNaN(v) {
			s += v
			n++
		}
	}
	if n == 0 {
		return math.NaN()
	}
	return s / float64(n)
}

func variance(vals []float64) float64 {
	m := mean(vals)
	if math.IsNaN(m) {
		return 0
	}
	s, n := 0.0, 0
	for _, v := range vals {
		if !math.IsNaN(v) {
			d := v - m
			s += d * d
			n++
		}
	}
	if n < 2 {
		return 0
	}
	return s / float64(n-1)
}

func median(vals []float64) float64 {
	var clean []float64
	for _, v := range vals {
		if !math.IsNaN(v) {
			clean = append(clean, v)
		}
	}
	if len(clean) == 0 {
		return math.NaN()
	}
	sort.Float64s(clean)
	mid := len(clean) / 2
	if len(clean)%2 == 0 {
		return (clean[mid-1] + clean[mid]) / 2.0
	}
	return clean[mid]
}

func cohensD(base, treat []float64) float64 {
	m1, m2 := mean(base), mean(treat)
	if math.IsNaN(m1) || math.IsNaN(m2) || len(base) < 2 || len(treat) < 2 {
		return 0
	}
	v1, v2 := variance(base), variance(treat)
	sd := math.Sqrt(((float64(len(base)-1) * v1) + (float64(len(treat)-1) * v2)) / float64(len(base)+len(treat)-2))
	if sd < 1e-6 {
		return 0
	}
	return (m1 - m2) / sd
}
