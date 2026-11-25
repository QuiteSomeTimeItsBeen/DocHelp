package main

import (
	"fmt"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// --- 1. Responsive Stick-Figure Leg Visualizer with Baseline Ghost ---

type stickLegWidget struct {
	widget.BaseWidget
	snap     GaitKinematicSnapshot
	ghost    GaitKinematicSnapshot
	hasGhost bool
	side     Leg
	cond     Condition
	hasVal   bool
}

func newStickLegWidget() *stickLegWidget {
	w := &stickLegWidget{cond: ConditionBase}
	w.ExtendBaseWidget(w)
	return w
}

func (w *stickLegWidget) SetPose(s GaitKinematicSnapshot, ghost GaitKinematicSnapshot, hasGhost bool, side Leg, cond Condition, ok bool) {
	w.snap, w.ghost, w.hasGhost, w.side, w.cond, w.hasVal = s, ghost, hasGhost, side, cond, ok
	w.Refresh()
}

func (w *stickLegWidget) MinSize() fyne.Size { return fyne.NewSize(200, 260) }

func (w *stickLegWidget) CreateRenderer() fyne.WidgetRenderer {
	return &stickLegRenderer{w: w}
}

type stickLegRenderer struct {
	w    *stickLegWidget
	objs []fyne.CanvasObject
}

func (r *stickLegRenderer) Destroy()                     {}
func (r *stickLegRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *stickLegRenderer) MinSize() fyne.Size           { return r.w.MinSize() }
func (r *stickLegRenderer) Refresh() {
	r.Layout(r.w.Size())
	canvas.Refresh(r.w)
}
func (r *stickLegRenderer) Layout(sz fyne.Size) {
	r.objs = nil
	if sz.Width < 80 || sz.Height < 80 {
		return
	}

	gridCol := color.NRGBA{130, 130, 145, 45}
	step := float32(28)
	for x := step; x < sz.Width; x += step {
		vLn := canvas.NewLine(gridCol)
		vLn.StrokeWidth = 1
		vLn.Position1 = fyne.NewPos(x, 0)
		vLn.Position2 = fyne.NewPos(x, sz.Height)
		r.objs = append(r.objs, vLn)
	}
	for y := step; y < sz.Height; y += step {
		hLn := canvas.NewLine(gridCol)
		hLn.StrokeWidth = 1
		hLn.Position1 = fyne.NewPos(0, y)
		hLn.Position2 = fyne.NewPos(sz.Width, y)
		r.objs = append(r.objs, hLn)
	}

	if !r.w.hasVal {
		return
	}

	scale := float32(math.Min(float64(sz.Width)/320.0, float64(sz.Height)/320.0))
	pelvisLen := 42 * scale
	gap := 10 * scale
	thighLen := 105 * scale
	shankLen := 100 * scale
	footLen := 42 * scale
	pelvis := fyne.NewPos(sz.Width*0.48, sz.Height*0.24)

	floorY := pelvis.Y + gap + thighLen + shankLen + 14*scale
	floor := canvas.NewLine(color.NRGBA{150, 150, 160, 120})
	floor.StrokeWidth = 1.5
	floor.Position1 = fyne.NewPos(sz.Width*0.12, floorY)
	floor.Position2 = fyne.NewPos(sz.Width*0.88, floorY)
	r.objs = append(r.objs, floor)

	addLine := func(p1, p2 fyne.Position, c color.Color, width float32) {
		ln := canvas.NewLine(c)
		ln.StrokeWidth = width
		ln.Position1, ln.Position2 = p1, p2
		r.objs = append(r.objs, ln)
	}

	computeJoints := func(s GaitKinematicSnapshot) (pFront, pBack, hip, knee, ankle, toe fyne.Position) {
		pRad := s.ASagP / 180 * math.Pi
		hRad := s.ASagH/180*math.Pi - math.Pi/2 - pRad
		kRad := hRad - s.ASagK/180*math.Pi
		aRad := kRad + math.Pi/2 - s.ASagA/180*math.Pi

		pFront = fyne.NewPos(pelvis.X+pelvisLen/2*float32(math.Cos(pRad)), pelvis.Y+pelvisLen/2*float32(math.Sin(pRad)))
		pBack = fyne.NewPos(pelvis.X-pelvisLen/2*float32(math.Cos(pRad)), pelvis.Y-pelvisLen/2*float32(math.Sin(pRad)))
		hip = fyne.NewPos(pelvis.X+gap*float32(math.Sin(pRad)), pelvis.Y+gap*float32(math.Cos(pRad)))
		knee = fyne.NewPos(hip.X+thighLen*float32(math.Cos(hRad)), hip.Y-thighLen*float32(math.Sin(hRad)))
		ankle = fyne.NewPos(knee.X+shankLen*float32(math.Cos(kRad)), knee.Y-shankLen*float32(math.Sin(kRad)))
		toe = fyne.NewPos(ankle.X+footLen*float32(math.Cos(aRad)), ankle.Y-footLen*float32(math.Sin(aRad)))
		return
	}

	if r.w.hasGhost && r.w.cond != ConditionBase {
		gCol := color.NRGBA{145, 155, 175, 85}
		gpF, gpB, gHip, gKnee, gAnkle, gToe := computeJoints(r.w.ghost)
		addLine(gpF, gpB, gCol, 3.0)
		addLine(gHip, gKnee, gCol, 2.2)
		addLine(gKnee, gAnkle, gCol, 2.2)
		addLine(gAnkle, gToe, gCol, 2.2)
		for _, pt := range []fyne.Position{gHip, gKnee, gAnkle} {
			dot := canvas.NewCircle(gCol)
			dot.Resize(fyne.NewSize(5, 5))
			dot.Move(fyne.NewPos(pt.X-2.5, pt.Y-2.5))
			r.objs = append(r.objs, dot)
		}
	}

	limbCol := color.NRGBA{45, 115, 240, 255}
	if r.w.side == Right {
		limbCol = color.NRGBA{235, 65, 55, 255}
	}
	pelvisCol := condColors[r.w.cond]
	if pelvisCol == nil {
		pelvisCol = condColors[ConditionBase]
	}

	s := r.w.snap
	pFront, pBack, hip, knee, ankle, toe := computeJoints(s)
	addLine(pFront, pBack, pelvisCol, 4.5)
	addLine(hip, knee, limbCol, 3.2)
	addLine(knee, ankle, limbCol, 3.2)
	addLine(ankle, toe, limbCol, 3.2)

	joints := []struct {
		pt  fyne.Position
		lbl string
	}{
		{hip, fmt.Sprintf("H: %.0f°", s.ASagH)},
		{knee, fmt.Sprintf("K: %.0f°", s.ASagK)},
		{ankle, fmt.Sprintf("A: %.0f°", s.ASagA)},
	}
	lblCol := color.NRGBA{165, 175, 190, 235}
	for _, j := range joints {
		dot := canvas.NewCircle(color.NRGBA{245, 245, 250, 255})
		dot.Resize(fyne.NewSize(7, 7))
		dot.Move(fyne.NewPos(j.pt.X-3.5, j.pt.Y-3.5))
		r.objs = append(r.objs, dot)

		txt := canvas.NewText(j.lbl, lblCol)
		txt.TextSize = 10
		txt.Move(fyne.NewPos(j.pt.X+8, j.pt.Y-6))
		r.objs = append(r.objs, txt)
	}
}

// --- 2. Interactive Clickable Discrete Graph with Outlier Rings & Norm Band ---

type discreteVarSpec struct {
	Code    string
	LabelEN string
	LabelRU string
	HasNorm bool
	NormLow float64
	NormHi  float64
	Extract func(GaitDiscreteParams) float64
}

func (d discreteVarSpec) Label(lang string) string {
	if lang == "RU" {
		return d.LabelRU
	}
	return d.LabelEN
}

var discreteVars = []discreteVarSpec{
	{"GPS_a", "Gait Profile Score (GPS)", "Индекс профиля походки (GPS)", true, 4.5, 6.2, func(d GaitDiscreteParams) float64 { return d.GPS_a }},
	{"GVS_aSagP", "Pelvis Tilt Deviation (GVS)", "Отклонение наклона таза (GVS)", true, 3.0, 5.5, func(d GaitDiscreteParams) float64 { return d.GVS_aSagP }},
	{"GVS_aSagH", "Hip Angle Deviation (GVS)", "Отклонение угла бедра (GVS)", true, 3.5, 6.0, func(d GaitDiscreteParams) float64 { return d.GVS_aSagH }},
	{"GVS_aSagK", "Knee Angle Deviation (GVS)", "Отклонение угла колена (GVS)", true, 4.0, 6.5, func(d GaitDiscreteParams) float64 { return d.GVS_aSagK }},
	{"GVS_aSagA", "Ankle Angle Deviation (GVS)", "Отклонение голеностопа (GVS)", true, 3.5, 6.0, func(d GaitDiscreteParams) float64 { return d.GVS_aSagA }},
	{"aMinSagH", "Min Hip Extension Angle", "Мин. угол разгибания бедра", false, 0, 0, func(d GaitDiscreteParams) float64 { return d.aMinSagH }},
	{"pctmZeroStSagH", "Hip Moment Zero-Crossing (% Step)", "Переход момента бедра через 0 (% шага)", false, 0, 0, func(d GaitDiscreteParams) float64 { return d.pctmZeroStSagH }},
}

var condColors = map[Condition]color.Color{
	ConditionBase:   color.NRGBA{52, 131, 235, 255},
	ConditionVerbal: color.NRGBA{46, 184, 92, 255},
	ConditionVR:     color.NRGBA{235, 64, 52, 255},
}

type plottedPoint struct {
	cond     Condition
	cycleIdx int
	pos      fyne.Position
}

type discreteChartWidget struct {
	widget.BaseWidget
	series      map[Condition][]float64
	evals       map[Condition][]CycleEval
	varSpec     discreteVarSpec
	activeCond  Condition
	activeCycle int
	points      []plottedPoint
	onSelect    func(Condition, int)
}

func newDiscreteChartWidget(onSelect func(Condition, int)) *discreteChartWidget {
	c := &discreteChartWidget{onSelect: onSelect, activeCond: ConditionBase}
	c.ExtendBaseWidget(c)
	return c
}

func (c *discreteChartWidget) SetData(s map[Condition][]float64, evals map[Condition][]CycleEval, spec discreteVarSpec, activeCond Condition, activeCycle int) {
	c.series, c.evals, c.varSpec, c.activeCond, c.activeCycle = s, evals, spec, activeCond, activeCycle
	c.Refresh()
}

func (c *discreteChartWidget) SetActive(cond Condition, cycleIdx int) {
	c.activeCond, c.activeCycle = cond, cycleIdx
	c.Refresh()
}

func (c *discreteChartWidget) Tapped(ev *fyne.PointEvent) {
	bestDist := float32(28.0)
	var hit *plottedPoint
	for i := range c.points {
		dx := c.points[i].pos.X - ev.Position.X
		dy := c.points[i].pos.Y - ev.Position.Y
		dist := float32(math.Hypot(float64(dx), float64(dy)))
		if dist < bestDist {
			bestDist = dist
			hit = &c.points[i]
		}
	}
	if hit != nil {
		c.activeCond = hit.cond
		c.activeCycle = hit.cycleIdx
		c.Refresh()
		if c.onSelect != nil {
			c.onSelect(hit.cond, hit.cycleIdx)
		}
	}
}

func (c *discreteChartWidget) MinSize() fyne.Size { return fyne.NewSize(300, 260) }
func (c *discreteChartWidget) CreateRenderer() fyne.WidgetRenderer {
	return &discreteChartRenderer{c: c}
}

type discreteChartRenderer struct {
	c    *discreteChartWidget
	objs []fyne.CanvasObject
}

func (r *discreteChartRenderer) Destroy()                     {}
func (r *discreteChartRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *discreteChartRenderer) MinSize() fyne.Size           { return r.c.MinSize() }
func (r *discreteChartRenderer) Refresh() {
	r.Layout(r.c.Size())
	canvas.Refresh(r.c)
}

func (r *discreteChartRenderer) Layout(sz fyne.Size) {
	r.objs = nil
	r.c.points = nil
	if sz.Width < 120 || sz.Height < 100 {
		return
	}

	const padL, padR, padT, padB = float32(52), float32(16), float32(14), float32(34)
	pL, pR, pT, pB := padL, sz.Width-padR, padT, sz.Height-padB
	pW, pH := pR-pL, pB-pT
	if pW <= 10 || pH <= 10 {
		return
	}

	maxCyc, minY, maxY := 0, math.Inf(1), math.Inf(-1)
	for _, vals := range r.c.series {
		if len(vals) > maxCyc {
			maxCyc = len(vals)
		}
		for _, v := range vals {
			if !math.IsNaN(v) {
				if v < minY {
					minY = v
				}
				if v > maxY {
					maxY = v
				}
			}
		}
	}
	if maxCyc == 0 || math.IsInf(minY, 1) {
		return
	}
	if r.c.varSpec.HasNorm {
		if r.c.varSpec.NormLow < minY {
			minY = r.c.varSpec.NormLow
		}
		if r.c.varSpec.NormHi > maxY {
			maxY = r.c.varSpec.NormHi
		}
	}
	if math.Abs(maxY-minY) < 1e-6 {
		minY -= 1
		maxY += 1
	} else {
		m := (maxY - minY) * 0.1
		minY -= m
		maxY += m
	}

	xFor := func(i int) float32 {
		if maxCyc == 1 {
			return pL + pW/2
		}
		return pL + (float32(i)/float32(maxCyc-1))*pW
	}
	yFor := func(v float64) float32 {
		return pB - float32((v-minY)/(maxY-minY))*pH
	}

	if r.c.varSpec.HasNorm {
		yTop := yFor(r.c.varSpec.NormHi)
		yBot := yFor(r.c.varSpec.NormLow)
		band := canvas.NewRectangle(color.NRGBA{46, 184, 92, 30})
		band.Move(fyne.NewPos(pL, yTop))
		band.Resize(fyne.NewSize(pW, yBot-yTop))
		r.objs = append(r.objs, band)
	}

	gridCol := color.NRGBA{130, 130, 140, 55}
	lblCol := color.NRGBA{140, 140, 150, 255}

	for i := 0; i <= 4; i++ {
		frac := float32(i) / 4.0
		y := pB - frac*pH
		ln := canvas.NewLine(gridCol)
		ln.Position1, ln.Position2 = fyne.NewPos(pL, y), fyne.NewPos(pR, y)
		r.objs = append(r.objs, ln)

		txt := canvas.NewText(fmt.Sprintf("%.1f", minY+float64(frac)*(maxY-minY)), lblCol)
		txt.TextSize = 10
		txt.Alignment = fyne.TextAlignTrailing
		txt.Resize(fyne.NewSize(padL-6, 14))
		txt.Move(fyne.NewPos(0, y-7))
		r.objs = append(r.objs, txt)
	}

	for i := 0; i < maxCyc; i++ {
		if maxCyc > 12 && i%2 != 0 {
			continue
		}
		x := xFor(i)
		txt := canvas.NewText(fmt.Sprintf("%d", i+1), lblCol)
		txt.TextSize = 10
		txt.Alignment = fyne.TextAlignCenter
		txt.Resize(fyne.NewSize(20, 14))
		txt.Move(fyne.NewPos(x-10, pB+4))
		r.objs = append(r.objs, txt)
	}

	for _, cond := range []Condition{ConditionBase, ConditionVerbal, ConditionVR} {
		vals := r.c.series[cond]
		col := condColors[cond]
		var prev *fyne.Position
		for i, v := range vals {
			if math.IsNaN(v) {
				prev = nil
				continue
			}
			pt := fyne.NewPos(xFor(i), yFor(v))
			r.c.points = append(r.c.points, plottedPoint{cond: cond, cycleIdx: i, pos: pt})
			if prev != nil {
				seg := canvas.NewLine(col)
				seg.StrokeWidth = 2
				seg.Position1, seg.Position2 = *prev, pt
				r.objs = append(r.objs, seg)
			}

			if r.c.evals != nil && i < len(r.c.evals[cond]) && r.c.evals[cond][i].IsBad {
				warnRing := canvas.NewCircle(color.Transparent)
				warnRing.StrokeColor = color.NRGBA{245, 170, 35, 240}
				warnRing.StrokeWidth = 2.0
				warnRing.Resize(fyne.NewSize(16, 16))
				warnRing.Move(fyne.NewPos(pt.X-8, pt.Y-8))
				r.objs = append(r.objs, warnRing)
			}

			rad := float32(4.0)
			if cond == r.c.activeCond && i == r.c.activeCycle {
				rad = 7.0
			}
			dot := canvas.NewCircle(col)
			dot.Resize(fyne.NewSize(rad*2, rad*2))
			dot.Move(fyne.NewPos(pt.X-rad, pt.Y-rad))
			r.objs = append(r.objs, dot)
			pCopy := pt
			prev = &pCopy
		}
	}
}
