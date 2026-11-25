package main

import (
	"fmt"
	"image/color"
	"io"
	"log"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type appState struct {
	fyneApp   fyne.App
	mainWin   fyne.Window
	sources   []CSVSource
	patients  []*Patient
	mlMetrics SubjectMLMetrics
	mlReady   bool
	lang      string
	darkTheme bool
	zThresh   float64
	activeTab int
	refreshUI func()
	rerunML   func()
}

func (st *appState) tr(en, ru string) string {
	if st.lang == "RU" {
		return ru
	}
	return en
}

func main() {
	sources, err := defaultCSVSources()
	if err != nil {
		log.Fatal(err)
	}

	patients, err := loadAllPatients(sources)
	if err != nil {
		log.Fatal(err)
	}

	a := app.New()
	mainWin := a.NewWindow("Doc. Helper 0.2")
	mainWin.Resize(fyne.NewSize(960, 650))

	st := &appState{
		fyneApp:   a,
		mainWin:   mainWin,
		sources:   sources,
		patients:  patients,
		lang:      "EN",
		darkTheme: true,
		zThresh:   2.5,
	}
	a.SetIcon(theme.HomeIcon())
	mainWin.SetIcon(theme.Current().Icon(theme.IconNameFolderNew))

	contentHolder := container.NewStack()

	renderTab := func(idx int) {
		st.activeTab = idx
		switch idx {
		case 0:
			contentHolder.Objects = []fyne.CanvasObject{buildPatientCardsTab(st)}
		case 1:
			contentHolder.Objects = []fyne.CanvasObject{buildCSVManagerTab(st)}
		case 2:
			contentHolder.Objects = []fyne.CanvasObject{buildSettingsTab(st)}
		case 3:
			contentHolder.Objects = []fyne.CanvasObject{buildAccuracyCitationsTab(st)}
		}
		contentHolder.Refresh()
	}

	var btnPatients, btnData, btnSettings, btnMetrics *widget.Button
	rebuildSidebarLabels := func() {
		btnPatients.SetText(st.tr("Patient Cards", "Карточки пациентов"))
		btnData.SetText(st.tr("Data & CSV", "Данные и CSV"))
		btnSettings.SetText(st.tr("Settings", "Настройки"))
		btnMetrics.SetText(st.tr("ML & Citations", "Метрики ИИ и источники"))
	}

	btnPatients = widget.NewButton("Patient Cards", func() { renderTab(0) })
	btnData = widget.NewButton("Data & CSV", func() { renderTab(1) })
	btnSettings = widget.NewButton("Settings", func() { renderTab(2) })
	btnMetrics = widget.NewButton("ML & Citations", func() { renderTab(3) })

	st.refreshUI = func() {
		rebuildSidebarLabels()
		renderTab(st.activeTab)
	}

	st.rerunML = func() {
		st.mlReady = false
		ptsCopy := append([]*Patient(nil), st.patients...)
		go func() {
			res := EvaluateSubjectSplitML(ptsCopy)
			fyne.Do(func() {
				st.mlMetrics = res
				st.mlReady = true
				if st.activeTab == 3 && st.refreshUI != nil {
					st.refreshUI()
				}
			})
		}()
	}

	st.rerunML()

	sidebar := container.NewVBox(
		widget.NewLabelWithStyle("Doc. helper 0.2", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		btnPatients,
		btnData,
		btnSettings,
		btnMetrics,
	)

	renderTab(0)
	mainWin.SetContent(container.NewBorder(nil, nil, container.NewPadded(sidebar), nil, contentHolder))
	mainWin.ShowAndRun()
}
func buildPatientCardsTab(st *appState) fyne.CanvasObject {
	cardsBox := container.NewVBox()
	unitCM := st.tr("cm", "см")
	unitKG := st.tr("kg", "кг")

	for _, p := range st.patients {
		patient := p
		analysis := AnalyzePatient(patient, st.zThresh)
		status := analysis.StatusEN
		if st.lang == "RU" {
			status = analysis.StatusRU
		}

		var stripeCol color.Color = color.NRGBA{110, 120, 135, 255}
		switch analysis.StatusKind {
		case 1:
			stripeCol = color.NRGBA{46, 184, 92, 255}
		case -1:
			stripeCol = color.NRGBA{235, 64, 52, 255}
		case 2:
			stripeCol = color.NRGBA{235, 165, 35, 255}
		}
		stripe := canvas.NewRectangle(stripeCol)
		stripe.SetMinSize(fyne.NewSize(6, 0))

		title := widget.NewLabelWithStyle(
			fmt.Sprintf("%s  (%s)", patient.DisplayName(st.lang), patient.SideLabel(st.lang)),
			fyne.TextAlignLeading, fyne.TextStyle{Bold: true},
		)
		meta := widget.NewLabel(fmt.Sprintf(
			"%s: %.0f %s  |  %s: %.1f %s  |  %s: %d/%d/%d",
			st.tr("Height", "Рост"), patient.HeightCM(), unitCM,
			st.tr("Weight", "Вес"), patient.Weight, unitKG,
			st.tr("Steps (B/Vb/VR)", "Шаги (Б/Вб/VR)"),
			len(patient.Params.Base), len(patient.Params.Verbal), len(patient.Params.VR),
		))
		statusLbl := widget.NewLabel(fmt.Sprintf("%s: %s", st.tr("Status", "Статус"), status))
		openBtn := widget.NewButton(st.tr("Open Card →", "Открыть →"), func() {
			openPatientCardWindow(st, patient)
		})

		card := widget.NewCard("", "", container.NewBorder(
			nil, nil, stripe, openBtn,
			container.NewPadded(container.NewVBox(title, meta, statusLbl)),
		))
		cardsBox.Add(card)
	}
	return container.NewVScroll(container.NewPadded(cardsBox))
}

func openPatientCardWindow(st *appState, p *Patient) {
	w := st.fyneApp.NewWindow(p.DisplayName(st.lang))
	w.Resize(fyne.NewSize(980, 680))

	analysis := AnalyzePatient(p, st.zThresh)
	summaryText := GenerateClinicalSummary(p, analysis, st.lang)

	unitCM := st.tr("cm", "см")
	unitKG := st.tr("kg", "кг")
	infoLbl := widget.NewLabelWithStyle(fmt.Sprintf(
		"%s | %s: %s | %s: %.0f %s | %s: %.1f %s | %s: %.1f %s",
		p.DisplayName(st.lang),
		st.tr("Side", "Сторона"), p.SideLabel(st.lang),
		st.tr("Height", "Рост"), p.HeightCM(), unitCM,
		st.tr("Weight", "Вес"), p.Weight, unitKG,
		st.tr("Leg", "Длина ноги"), p.LegLengthCM(), unitCM,
	), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	exportBtn := widget.NewButton(st.tr("Save Report (.html)", "Сохранить отчёт (.html)"), func() {
		saveDlg := dialog.NewFileSave(func(uc fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if uc == nil {
				return
			}
			defer uc.Close()
			html := BuildPatientReportHTML(p, analysis, summaryText, st.lang)
			if _, wErr := uc.Write([]byte(html)); wErr != nil {
				dialog.ShowError(wErr, w)
				return
			}
			dialog.ShowInformation(st.tr("Saved", "Сохранено"), st.tr("Report saved successfully.", "Отчёт успешно сохранён."), w)
		}, w)
		saveDlg.SetFileName(fmt.Sprintf("report_%s.html", p.Subject))
		saveDlg.Show()
	})

	topHeader := container.NewBorder(nil, widget.NewSeparator(), infoLbl, exportBtn)

	firstIdx := 0
	selCond := ConditionBase
	selCycleIdx := 0
	selVar := discreteVars[firstIdx]
	ignoreCondChange := false
	showGhost := true

	legVis := newStickLegWidget()
	cycleInfoLbl := widget.NewLabel("")
	pointDetailLbl := widget.NewLabel(st.tr("Click any point to jump to that step →", "Нажмите на точку графика для перехода к шагу →"))
	slider := widget.NewSlider(0, 50)
	slider.Step = 1

	var chart *discreteChartWidget
	var condSelect *widget.Select

	updatePointDetail := func() {
		cycles := p.GetCycles(selCond)
		if selCycleIdx >= len(cycles) {
			return
		}
		val := selVar.Extract(cycles[selCycleIdx].Discrete)
		msg := fmt.Sprintf("%s — %s %d: %.2f", selCond.Label(st.lang), st.tr("Step", "Шаг"), selCycleIdx+1, val)
		if evs := analysis.CycleEvals[selCond]; selCycleIdx < len(evs) && evs[selCycleIdx].IsBad {
			msg += fmt.Sprintf("  ⚠ %s (%s)", st.tr("Unstable", "Нестабильный"), strings.Join(evs[selCycleIdx].Reasons, ", "))
		}
		pointDetailLbl.SetText(msg)
	}

	renderPose := func(frame int) {
		cycles := p.GetCycles(selCond)
		if len(cycles) == 0 || selCycleIdx >= len(cycles) || len(cycles[selCycleIdx].Snapshots) == 0 {
			legVis.SetPose(GaitKinematicSnapshot{}, GaitKinematicSnapshot{}, false, p.ParseLeg(), selCond, false)
			cycleInfoLbl.SetText(st.tr("No kinematic data", "Нет кинематических данных"))
			return
		}
		snaps := cycles[selCycleIdx].Snapshots
		if frame < 0 {
			frame = 0
		}
		if frame >= len(snaps) {
			frame = len(snaps) - 1
		}
		s := snaps[frame]

		var ghostSnap GaitKinematicSnapshot
		hasGhost := false
		if showGhost && selCond != ConditionBase && len(p.Params.Base) > 0 && len(p.Params.Base[0].Snapshots) > 0 {
			bSnaps := p.Params.Base[0].Snapshots
			bIdx := int(float64(frame) / float64(len(snaps)) * float64(len(bSnaps)))
			if bIdx >= len(bSnaps) {
				bIdx = len(bSnaps) - 1
			}
			ghostSnap = bSnaps[bIdx]
			hasGhost = true
		}

		legVis.SetPose(s, ghostSnap, hasGhost, p.ParseLeg(), selCond, true)

		pctVal := float64(s.PctGC)
		if pctVal > 100 {
			pctVal = pctVal / 10.0
		}
		cycleInfoLbl.SetText(fmt.Sprintf("%s | %s %d/%d | %.1f%% %s",
			selCond.Label(st.lang), st.tr("Step", "Шаг"), selCycleIdx+1, len(cycles),
			pctVal, st.tr("GC", "цикла шага")))
	}
	syncToConditionAndCycle := func(cond Condition, cycleIdx int, fromDropdown bool) {
		selCond = cond
		cycles := p.GetCycles(selCond)
		if cycleIdx < 0 {
			cycleIdx = 0
		}
		if len(cycles) > 0 && cycleIdx >= len(cycles) {
			cycleIdx = len(cycles) - 1
		}
		selCycleIdx = cycleIdx

		if !fromDropdown && condSelect != nil {
			ignoreCondChange = true
			condSelect.SetSelected(selCond.Label(st.lang))
			ignoreCondChange = false
		}
		if len(cycles) > 0 && len(cycles[selCycleIdx].Snapshots) > 0 {
			slider.Max = float64(len(cycles[selCycleIdx].Snapshots) - 1)
			slider.SetValue(0)
		}
		if chart != nil {
			chart.SetActive(selCond, selCycleIdx)
		}
		updatePointDetail()
		renderPose(int(slider.Value))
	}

	chart = newDiscreteChartWidget(func(clickedCond Condition, clickedCycle int) {
		syncToConditionAndCycle(clickedCond, clickedCycle, false)
	})

	refreshChartSeries := func() {
		series := map[Condition][]float64{
			ConditionBase:   extractVals(p.Params.Base, selVar.Extract),
			ConditionVerbal: extractVals(p.Params.Verbal, selVar.Extract),
			ConditionVR:     extractVals(p.Params.VR, selVar.Extract),
		}
		chart.SetData(series, analysis.CycleEvals, selVar, selCond, selCycleIdx)
		updatePointDetail()
	}

	varNames := make([]string, len(discreteVars))
	for i, v := range discreteVars {
		varNames[i] = v.Label(st.lang)
	}
	varSelect := widget.NewSelect(varNames, nil)
	varSelect.SetSelected(varNames[firstIdx])
	varSelect.OnChanged = func(val string) {
		for _, v := range discreteVars {
			if v.Label(st.lang) == val {
				selVar = v
				refreshChartSeries()
				break
			}
		}
	}

	legend := container.NewHBox(
		legendBadge(ConditionBase.Label(st.lang), condColors[ConditionBase]),
		legendBadge(ConditionVerbal.Label(st.lang), condColors[ConditionVerbal]),
		legendBadge(ConditionVR.Label(st.lang), condColors[ConditionVR]),
	)

	pointDetailLbl.Wrapping = fyne.TextWrapWord

	graphTopBar := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel(st.tr("Param:", "Параметр:")), nil, varSelect),
		legend,
	)

	leftGraphPane := container.NewBorder(
		graphTopBar,
		pointDetailLbl,
		nil, nil,
		chart,
	)

	condOptions := []string{
		ConditionBase.Label(st.lang),
		ConditionVerbal.Label(st.lang),
		ConditionVR.Label(st.lang),
	}
	condSelect = widget.NewSelect(condOptions, nil)
	condSelect.SetSelected(ConditionBase.Label(st.lang))
	condSelect.OnChanged = func(val string) {
		if ignoreCondChange {
			return
		}
		switch val {
		case ConditionVerbal.Label(st.lang):
			syncToConditionAndCycle(ConditionVerbal, 0, true)
		case ConditionVR.Label(st.lang):
			syncToConditionAndCycle(ConditionVR, 0, true)
		default:
			syncToConditionAndCycle(ConditionBase, 0, true)
		}
	}

	ghostCheck := widget.NewCheck(st.tr("Baseline Ghost", "Фантом базы"), func(on bool) {
		showGhost = on
		renderPose(int(slider.Value))
	})
	ghostCheck.SetChecked(true)

	prevCycleBtn := widget.NewButton("◀", func() {
		if selCycleIdx > 0 {
			syncToConditionAndCycle(selCond, selCycleIdx-1, true)
		}
	})
	nextCycleBtn := widget.NewButton("▶", func() {
		cycles := p.GetCycles(selCond)
		if selCycleIdx+1 < len(cycles) {
			syncToConditionAndCycle(selCond, selCycleIdx+1, true)
		}
	})

	slider.OnChanged = func(v float64) { renderPose(int(v)) }

	playing := false
	var stopCh chan struct{}
	var playBtn *widget.Button
	playBtn = widget.NewButton(st.tr("Play", "Старт"), func() {
		if playing {
			playing = false
			close(stopCh)
			playBtn.SetText(st.tr("Play", "Старт"))
			return
		}
		playing = true
		stopCh = make(chan struct{})
		playBtn.SetText(st.tr("Pause", "Пауза"))
		go func(done <-chan struct{}) {
			tk := time.NewTicker(40 * time.Millisecond)
			defer tk.Stop()
			for {
				select {
				case <-done:
					return
				case <-tk.C:
					fyne.Do(func() {
						nxt := slider.Value + 1
						if nxt > slider.Max {
							nxt = 0
						}
						slider.SetValue(nxt)
					})
				}
			}
		}(stopCh)
	})

	w.SetOnClosed(func() {
		if playing {
			close(stopCh)
		}
	})

	visTopBar := container.NewVBox(
		container.NewBorder(nil, nil, condSelect, container.NewHBox(prevCycleBtn, nextCycleBtn, ghostCheck)),
		cycleInfoLbl,
	)

	rightVisPane := container.NewBorder(
		visTopBar,
		container.NewBorder(nil, nil, playBtn, nil, slider),
		nil, nil,
		legVis,
	)

	middleSplit := container.NewGridWithColumns(2, leftGraphPane, rightVisPane)

	showGlossary := false
	summaryLabel := widget.NewLabel(summaryText)
	summaryLabel.Wrapping = fyne.TextWrapWord

	var toggleGlossaryBtn *widget.Button
	toggleGlossaryBtn = widget.NewButton(st.tr("Show Term Glossary (?)", "Справка по терминам (?)"), func() {
		showGlossary = !showGlossary
		if showGlossary {
			summaryLabel.SetText(summaryText + "\n\n" + GetClinicalGlossary(st.lang))
			toggleGlossaryBtn.SetText(st.tr("Hide Term Glossary", "Скрыть справку"))
		} else {
			summaryLabel.SetText(summaryText)
			toggleGlossaryBtn.SetText(st.tr("Show Term Glossary (?)", "Справка по терминам (?)"))
		}
	})

	summaryScroll := container.NewVScroll(summaryLabel)
	summaryScroll.SetMinSize(fyne.NewSize(0, 175))
	bottomSummary := container.NewBorder(
		container.NewBorder(nil, nil, nil, toggleGlossaryBtn),
		nil, nil, nil,
		summaryScroll,
	)

	refreshChartSeries()
	syncToConditionAndCycle(ConditionBase, 0, true)

	w.SetContent(container.NewPadded(container.NewBorder(topHeader, bottomSummary, nil, nil, middleSplit)))
	w.Show()
}

func buildCSVManagerTab(st *appState) fyne.CanvasObject {
	selectedIdx := 0
	var tableData [][]string
	if len(st.sources) > 0 {
		tableData, _ = parseCSVTable(st.sources[0].Raw)
	}

	statusLbl := widget.NewLabel("")
	updateStatus := func() {
		if selectedIdx < 0 || selectedIdx >= len(st.sources) {
			return
		}
		src := st.sources[selectedIdx]
		origin := st.tr("Built-in", "Встроенный")
		if src.Custom {
			origin = st.tr("Uploaded", "Загружен")
		}
		rowsCount := 0
		if len(tableData) > 0 {
			rowsCount = len(tableData) - 1
		}
		statusLbl.SetText(fmt.Sprintf("%s: %s  |  %s: %d  |  [%s]",
			st.tr("File", "Файл"), src.Name,
			st.tr("Rows", "Строк"), rowsCount, origin))
	}
	updateStatus()

	csvTable := widget.NewTable(
		func() (int, int) {
			if len(tableData) == 0 {
				return 0, 0
			}
			cols := len(tableData[0])
			if cols > 14 {
				cols = 14
			}
			return len(tableData), cols
		},
		func() fyne.CanvasObject {
			lbl := widget.NewLabel("sample_cell_text")
			lbl.Truncation = fyne.TextTruncateEllipsis
			return lbl
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			lbl := obj.(*widget.Label)
			if id.Row < len(tableData) && id.Col < len(tableData[id.Row]) {
				lbl.SetText(tableData[id.Row][id.Col])
			} else {
				lbl.SetText("")
			}
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
			} else {
				lbl.TextStyle = fyne.TextStyle{}
			}
		},
	)
	for c := 0; c < 14; c++ {
		csvTable.SetColumnWidth(c, 110)
	}

	slotNames := make([]string, len(st.sources))
	for i, s := range st.sources {
		kind := st.tr("Discrete", "Дискретные")
		if s.Type == Kinematics {
			kind = st.tr("Kinematics", "Кинематика")
		}
		slotNames[i] = fmt.Sprintf("%s — %s (%s)", s.Condition.Label(st.lang), kind, s.Name)
	}

	slotSelect := widget.NewSelect(slotNames, func(val string) {
		for i, name := range slotNames {
			if name == val {
				selectedIdx = i
				tableData, _ = parseCSVTable(st.sources[i].Raw)
				updateStatus()
				csvTable.Refresh()
				break
			}
		}
	})
	if len(slotNames) > 0 {
		slotSelect.SetSelected(slotNames[0])
	}

	uploadBtn := widget.NewButton(st.tr("Upload CSV for Slot...", "Загрузить CSV..."), func() {
		fd := dialog.NewFileOpen(func(uc fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, st.mainWin)
				return
			}
			if uc == nil {
				return
			}
			defer uc.Close()
			raw, readErr := io.ReadAll(uc)
			if readErr != nil {
				dialog.ShowError(readErr, st.mainWin)
				return
			}

			trial := make([]CSVSource, len(st.sources))
			copy(trial, st.sources)
			trial[selectedIdx].Raw = raw
			trial[selectedIdx].Name = uc.URI().Name()
			trial[selectedIdx].Custom = true

			newPatients, parseErr := loadAllPatients(trial)
			if parseErr != nil || len(newPatients) == 0 {
				dialog.ShowError(fmt.Errorf("invalid gait CSV format: %v", parseErr), st.mainWin)
				return
			}

			st.sources = trial
			st.patients = newPatients
			st.rerunML()
			st.refreshUI()
		}, st.mainWin)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".csv"}))
		fd.Show()
	})

	resetBtn := widget.NewButton(st.tr("Reset All to Built-in", "Сбросить к встроенным"), func() {
		defSrc, err := defaultCSVSources()
		if err != nil {
			dialog.ShowError(err, st.mainWin)
			return
		}
		pts, err := loadAllPatients(defSrc)
		if err != nil {
			dialog.ShowError(err, st.mainWin)
			return
		}
		st.sources = defSrc
		st.patients = pts
		st.rerunML()
		st.refreshUI()
	})

	topControls := container.NewVBox(
		widget.NewLabelWithStyle(st.tr("CSV Dataset Viewer & Uploader", "Просмотр и загрузка CSV-файлов"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		container.NewBorder(nil, nil, widget.NewLabel(st.tr("Dataset Slot:", "Набор:")), container.NewHBox(uploadBtn, resetBtn), slotSelect),
		statusLbl,
	)

	return container.NewPadded(container.NewBorder(topControls, nil, nil, nil, csvTable))
}
func extractVals(cycles []GaitCycle, fn func(GaitDiscreteParams) float64) []float64 {
	out := make([]float64, len(cycles))
	for i, c := range cycles {
		out[i] = fn(c.Discrete)
	}
	return out
}

func legendBadge(name string, col color.Color) fyne.CanvasObject {
	rect := canvas.NewRectangle(col)
	rect.SetMinSize(fyne.NewSize(10, 10))
	return container.NewHBox(rect, widget.NewLabel(name))
}

func buildSettingsTab(st *appState) fyne.CanvasObject {
	langSelect := widget.NewSelect([]string{"English (EN)", "Русский (RU)"}, nil)
	if st.lang == "RU" {
		langSelect.SetSelected("Русский (RU)")
	} else {
		langSelect.SetSelected("English (EN)")
	}
	langSelect.OnChanged = func(val string) {
		if strings.Contains(val, "RU") {
			st.lang = "RU"
		} else {
			st.lang = "EN"
		}
		st.refreshUI()
	}

	themeSelect := widget.NewSelect([]string{"Dark", "Light"}, nil)
	if st.darkTheme {
		themeSelect.SetSelected("Dark")
	} else {
		themeSelect.SetSelected("Light")
	}
	themeSelect.OnChanged = func(val string) {
		st.darkTheme = (val == "Dark")
		if st.darkTheme {
			st.fyneApp.Settings().SetTheme(theme.DarkTheme())
		} else {
			st.fyneApp.Settings().SetTheme(theme.LightTheme())
		}
		st.refreshUI()
	}

	zSlider := widget.NewSlider(1.5, 4.0)
	zSlider.Step = 0.5
	zSlider.Value = st.zThresh
	zValLbl := widget.NewLabel(fmt.Sprintf("%.1f σ", st.zThresh))
	zSlider.OnChanged = func(v float64) {
		st.zThresh = v
		zValLbl.SetText(fmt.Sprintf("%.1f σ", v))
	}

	form := widget.NewForm(
		widget.NewFormItem(st.tr("Interface & Summary Language", "Язык интерфейса и отчётов"), langSelect),
		widget.NewFormItem(st.tr("Color Theme", "Тема оформления"), themeSelect),
		widget.NewFormItem(st.tr("Outlier Sensitivity (MAD Z-Score)", "Порог детектора нестабильных шагов"), container.NewBorder(nil, nil, nil, zValLbl, zSlider)),
	)

	return container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle(st.tr("Application Settings", "Настройки приложения"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		form,
	))
}

func buildAccuracyCitationsTab(st *appState) fyne.CanvasObject {
	if !st.mlReady {
		return container.NewCenter(widget.NewLabel(st.tr(
			"Running Leave-One-Subject-Out ML validation...",
			"Выполняется кросс-валидация модели по участникам...",
		)))
	}

	m := st.mlMetrics
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n", st.tr(
		"1. SUBJECT-SPLIT ML VALIDATION (LEAVE-ONE-SUBJECT-OUT CV)",
		"1. ВАЛИДАЦИЯ ML-МОДЕЛИ С РАЗДЕЛЕНИЕМ ПО СУБЪЕКТУ (МЕТОД ИСКЛЮЧЕНИЯ ПО ОДНОМУ СУБЪЕКТУ)",
	))
	fmt.Fprintf(&b, "%s: %d | %s: %d\n",
		st.tr("Subjects", "Участников"), m.Subjects,
		st.tr("Total step cycles", "Всего циклов шага"), m.Cycles)
	fmt.Fprintf(&b, "%s: %.1f%%   |   %s: %.1f%%\n", st.tr("Out-of-Fold accuracy", "Точность вне обучающего фолда"), m.Accuracy, st.tr("Macro-f1", "Макро-усредненная F1-мера"), m.MacroF1)
	fmt.Fprintf(&b, "%s - %s: %.1f%% | %s: %.1f%% | %s: %.1f%%\n\n",
		st.tr("Per-Class F1", "Поклассовая мера F1"),
		ConditionBase.Label(st.lang), m.ClassF1[clsBase],
		ConditionVerbal.Label(st.lang), m.ClassF1[clsVerbal],
		ConditionVR.Label(st.lang), m.ClassF1[clsVR])

	b.WriteString(st.tr("Confusion Matrix (Actual Rows vs. Predicted Cols):\n", "Матрица ошибок (Строки = ожидание, Столбцы = прогноз):\n"))
	fmt.Fprintf(&b, "            %-8s %-10s %s\n", "Base", "Verbal", "VR")
	names := []string{
		fmt.Sprintf("%-10s", "Base"),
		fmt.Sprintf("%-10s", "Verbal"),
		fmt.Sprintf("%-10s", "VR"),
	}

	for r := 0; r < numCls; r++ {
		fmt.Fprintf(&b, "%s %4d     %4d       %4d\n",
			names[r], m.Confusion[r][clsBase], m.Confusion[r][clsVerbal], m.Confusion[r][clsVR])
	}

	b.WriteString(st.tr("\nTop Most Discriminative Gait Features:\n", "\nНаиболее значимые признаки походки:\n"))

	for i := 0; i < min(5, len(m.TopFeatures)); i++ {
		fLabel := humanFeatureName(m.TopFeatures[i].Name, st.lang)
		fmt.Fprintf(&b, "  %d. %-36s (%.1f%%)\n", i+1, fLabel, m.TopFeatures[i].Importance)
	}

	b.WriteString(st.tr(
		"\n2. SOFTWARE & SCIENTIFIC CITATIONS\n"+
			"• Go Programming Language (v1.22+) - The Go Authors, BSD-3-Clause License (https://go.dev)\n"+
			"• Fyne Cross-Platform GUI Toolkit (v2) - Fyne.io Authors, BSD-3-Clause License (https://fyne.io)\n"+
			"• Gait Profile Score (GPS) & Gait Variable Score (GVS) - Baker et al., Gait & Posture, 2009\n"+
			"• Robust MAD Outlier Detection - Leys et al., Journal of Experimental Social Psychology, 2013\n"+
			"• Random Forests Classifier - Breiman, Machine Learning, 2001\n"+
			"• WHO ICF Categories b770 (Gait pattern functions) & d450 (Walking) - World Health Organization",
		"\n2. ИСПОЛЬЗОВАННОЕ ПО И НАУЧНЫЕ ИСТОЧНИКИ\n"+
			"• Язык программирования Go (v1.22+) - The Go Authors, лицензия BSD-3-Clause (https://go.dev)\n"+
			"• GUI-фреймворк Fyne (v2) - Fyne.io Authors, лицензия BSD-3-Clause (https://fyne.io)\n"+
			"• Индексы GPS и GVS - Baker et al., Gait & Posture, 2009\n"+
			"• Robust MAD Outlier Detection (Median/MAD) - Leys et al., JESP, 2013\n"+
			"• Алгоритм Random Forest - Breiman, Machine Learning, 2001\n"+
			"• Категории МКФ b770 (Функции стереотипа походки) и d450 (Ходьба) - ВОЗ",
	))

	lbl := widget.NewLabel(b.String())
	lbl.Wrapping = fyne.TextWrapWord

	return container.NewVScroll(container.NewPadded(lbl))
}
