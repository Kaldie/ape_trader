package main

import (
	"ape-trader/internal/market"
	"ape-trader/internal/models"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	chartWidth  = 840.0
	chartHeight = 240.0
	marginLeft  = 56.0
	marginRight = 24.0
	marginTop   = 18.0
	marginBot   = 34.0
)

type sample struct {
	Minute int
	Buy    int64
	Sell   int64
}

type lineSeries struct {
	ItemID      string
	ItemName    string
	Color       string
	BuyPath     string
	SellPath    string
	WasChanging bool
}

type townChart struct {
	TownID          string
	TownName        string
	Series          []lineSeries
	YAxisTicks      []axisTick
	MinuteLabels    []axisTick
	LegendRows      []legendRow
	AnySeriesChange bool
}

type axisTick struct {
	Label string
	X     float64
	Y     float64
}

type legendRow struct {
	ItemName string
	Color    string
}

type reportData struct {
	GeneratedAt     string
	InputFile       string
	Minutes         int
	Charts          []townChart
	StaticPriceNote bool
}

//go:embed templates/price_graph.html.tmpl
var templateFS embed.FS

func main() {
	inputPath := flag.String("input", "towns.json", "path to towns JSON input")
	outputPath := flag.String("output", "artifacts/price_graph.html", "path to generated HTML report")
	minutes := flag.Int("minutes", 30, "number of simulated minutes to plot")
	flag.Parse()

	if *minutes < 0 {
		fail(fmt.Errorf("minutes must be non-negative"))
	}

	towns, err := market.LoadTownsFromJSON(*inputPath)
	if err != nil {
		fail(err)
	}
	engine := market.NewMarketEngineWithTowns(towns)
	report, err := buildReport(*inputPath, *minutes, engine)
	if err != nil {
		fail(err)
	}

	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		fail(err)
	}
	if err := writeReport(*outputPath, report); err != nil {
		fail(err)
	}

	fmt.Printf("Wrote simulated no-trader line graph to %s\n", *outputPath)
}

func buildReport(inputPath string, minutes int, engine *market.MarketEngine) (reportData, error) {
	townSamples := make(map[string]map[models.ResourceID][]sample)
	townNames := make(map[string]string)

	for minute := 0; minute <= minutes; minute++ {
		if minute > 0 {
			engine.SimulateMinuteTick()
		}

		for townID, town := range engine.Towns {
			townNames[townID] = town.Name
			if _, ok := townSamples[townID]; !ok {
				townSamples[townID] = make(map[models.ResourceID][]sample)
			}
			prices, err := engine.CurrentPrices(town, nil)
			if err != nil {
				return reportData{}, err
			}
			for _, price := range prices {
				townSamples[townID][price.Resource] = append(townSamples[townID][price.Resource], sample{
					Minute: minute,
					Buy:    int64(price.Buy),
					Sell:   int64(price.Sell),
				})
			}
		}
	}

	townIDs := make([]string, 0, len(townSamples))
	for townID := range townSamples {
		townIDs = append(townIDs, townID)
	}
	sort.Slice(townIDs, func(i, j int) bool {
		return townNames[townIDs[i]] < townNames[townIDs[j]]
	})

	charts := make([]townChart, 0, len(townIDs))
	staticPriceNote := true
	for _, townID := range townIDs {
		seriesByResource := townSamples[townID]

		resourceIDs := make([]string, 0, len(seriesByResource))
		for resourceID := range seriesByResource {
			resourceIDs = append(resourceIDs, string(resourceID))
		}
		sort.Strings(resourceIDs)

		maxPrice := int64(1)
		for _, resourceKey := range resourceIDs {
			for _, point := range seriesByResource[models.ResourceID(resourceKey)] {
				if point.Buy > maxPrice {
					maxPrice = point.Buy
				}
				if point.Sell > maxPrice {
					maxPrice = point.Sell
				}
			}
		}

		chart := townChart{
			TownID:       townID,
			TownName:     townNames[townID],
			YAxisTicks:   buildYAxisTicks(maxPrice),
			MinuteLabels: buildMinuteTicks(minutes),
		}

		for i, resourceKey := range resourceIDs {
			resourceID := models.ResourceID(resourceKey)
			points := seriesByResource[resourceID]
			color := palette(i)
			changed := seriesChanged(points)
			if changed {
				staticPriceNote = false
				chart.AnySeriesChange = true
			}
			chart.Series = append(chart.Series, lineSeries{
				ItemID:      resourceKey,
				ItemName:    resourceKey,
				Color:       color,
				BuyPath:     buildPath(points, maxPrice, minutes, true),
				SellPath:    buildPath(points, maxPrice, minutes, false),
				WasChanging: changed,
			})
			chart.LegendRows = append(chart.LegendRows, legendRow{
				ItemName: resourceKey,
				Color:    color,
			})
		}

		charts = append(charts, chart)
	}

	return reportData{
		GeneratedAt:     time.Now().Format(time.RFC3339),
		InputFile:       inputPath,
		Minutes:         minutes,
		Charts:          charts,
		StaticPriceNote: staticPriceNote,
	}, nil
}

func buildPath(points []sample, maxPrice int64, totalMinutes int, buy bool) string {
	parts := make([]string, 0, len(points))
	for _, point := range points {
		price := point.Sell
		if buy {
			price = point.Buy
		}
		parts = append(parts, fmt.Sprintf("%.2f,%.2f", plotX(point.Minute, totalMinutes), plotY(price, maxPrice)))
	}
	return strings.Join(parts, " ")
}

func plotX(minute, totalMinutes int) float64 {
	if totalMinutes <= 0 {
		return marginLeft
	}
	return marginLeft + (float64(minute)/float64(totalMinutes))*(chartWidth-marginLeft-marginRight)
}

func plotY(price, maxPrice int64) float64 {
	if maxPrice <= 0 {
		return chartHeight - marginBot
	}
	usableHeight := chartHeight - marginTop - marginBot
	return chartHeight - marginBot - (float64(price)/float64(maxPrice))*usableHeight
}

func buildYAxisTicks(maxPrice int64) []axisTick {
	steps := []int64{0, maxPrice / 4, maxPrice / 2, (maxPrice * 3) / 4, maxPrice}
	ticks := make([]axisTick, 0, len(steps))
	seen := make(map[int64]bool)
	for _, step := range steps {
		if seen[step] {
			continue
		}
		seen[step] = true
		ticks = append(ticks, axisTick{
			Label: fmt.Sprintf("%d", step),
			X:     marginLeft - 8,
			Y:     plotY(step, maxPrice),
		})
	}
	sort.Slice(ticks, func(i, j int) bool {
		return ticks[i].Y > ticks[j].Y
	})
	return ticks
}

func buildMinuteTicks(totalMinutes int) []axisTick {
	if totalMinutes <= 0 {
		return []axisTick{{Label: "0", X: marginLeft, Y: chartHeight - 10}}
	}

	candidates := []int{0, totalMinutes / 4, totalMinutes / 2, (totalMinutes * 3) / 4, totalMinutes}
	ticks := make([]axisTick, 0, len(candidates))
	seen := make(map[int]bool)
	for _, minute := range candidates {
		if seen[minute] {
			continue
		}
		seen[minute] = true
		ticks = append(ticks, axisTick{
			Label: fmt.Sprintf("%d", minute),
			X:     plotX(minute, totalMinutes),
			Y:     chartHeight - 10,
		})
	}
	sort.Slice(ticks, func(i, j int) bool {
		return ticks[i].X < ticks[j].X
	})
	return ticks
}

func seriesChanged(points []sample) bool {
	if len(points) < 2 {
		return false
	}
	firstBuy := points[0].Buy
	firstSell := points[0].Sell
	for _, point := range points[1:] {
		if point.Buy != firstBuy || point.Sell != firstSell {
			return true
		}
	}
	return false
}

func palette(index int) string {
	colors := []string{
		"#c24e3f",
		"#2a6f97",
		"#5c8a3a",
		"#8a4fff",
		"#c7871b",
		"#1f8a70",
		"#9d4edd",
		"#b56576",
		"#577590",
		"#bc4749",
	}
	return colors[index%len(colors)]
}

func writeReport(path string, report reportData) error {
	tmpl, err := template.ParseFS(templateFS, "templates/price_graph.html.tmpl")
	if err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, report)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
