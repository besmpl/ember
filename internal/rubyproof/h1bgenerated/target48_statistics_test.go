package h1bgenerated

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"testing"
)

const (
	target48PairCount          = 30
	target48BootstrapResamples = 100_000
	target48BootstrapSeed      = uint64(0x5434384243413939) // "T48BCA99"
	target48MADScale           = 1.482602218505602
)

var (
	errTarget48NonFinite       = errors.New("target48: non-finite analysis input")
	errTarget48Collinear       = errors.New("target48: nonconstant nuisance predictors are collinear")
	errTarget48WrongSampleSize = errors.New("target48: analysis requires exactly 30 pairs")
)

type target48Interval struct {
	Point, Lower, Upper float64
	BiasZ, Acceleration float64
	LowerProbability    float64
	UpperProbability    float64
}

// target48SplitMix64 is protocol-owned rather than math/rand-owned so a Go
// library change cannot silently change a retained interval.
type target48SplitMix64 uint64

func (s *target48SplitMix64) next() uint64 {
	*s += 0x9e3779b97f4a7c15
	z := uint64(*s)
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (s *target48SplitMix64) index(bound uint64) uint64 {
	// Rejection avoids modulo bias over the complete uint64 output space.
	threshold := -bound % bound
	for {
		r := s.next()
		if r >= threshold {
			return r % bound
		}
	}
}

func target48Median(values []float64) (float64, error) {
	if len(values) == 0 {
		return 0, errors.New("target48: median of empty sample")
	}
	ordered := append([]float64(nil), values...)
	for _, value := range ordered {
		if !isFinite48(value) {
			return 0, errTarget48NonFinite
		}
	}
	sort.Float64s(ordered)
	mid := len(ordered) / 2
	if len(ordered)&1 != 0 {
		return ordered[mid], nil
	}
	return (ordered[mid-1] + ordered[mid]) / 2, nil
}

func target48QuantileType7(ordered []float64, probability float64) (float64, error) {
	if len(ordered) == 0 || !isFinite48(probability) || probability < 0 || probability > 1 {
		return 0, fmt.Errorf("target48: invalid type-7 quantile input")
	}
	if probability == 0 {
		return ordered[0], nil
	}
	if probability == 1 {
		return ordered[len(ordered)-1], nil
	}
	h := float64(len(ordered)-1) * probability
	lo := int(math.Floor(h))
	fraction := h - float64(lo)
	return ordered[lo] + fraction*(ordered[lo+1]-ordered[lo]), nil
}

func target48NormalCDF(z float64) float64 {
	return 0.5 * math.Erfc(-z/math.Sqrt2)
}

func target48NormalInverse(probability float64) float64 {
	switch probability {
	case 0:
		return math.Inf(-1)
	case 1:
		return math.Inf(1)
	default:
		return math.Sqrt2 * math.Erfinv(2*probability-1)
	}
}

func target48BCa99(logRatios []float64) (target48Interval, error) {
	if len(logRatios) != target48PairCount {
		return target48Interval{}, errTarget48WrongSampleSize
	}
	pointLog, err := target48Median(logRatios)
	if err != nil {
		return target48Interval{}, err
	}
	constant := true
	for _, value := range logRatios[1:] {
		if value != logRatios[0] {
			constant = false
			break
		}
	}
	if constant {
		point := math.Exp(pointLog)
		if !isFinite48(point) {
			return target48Interval{}, errTarget48NonFinite
		}
		return target48Interval{
			Point: point, Lower: point, Upper: point,
			LowerProbability: 0.005, UpperProbability: 0.995,
		}, nil
	}

	bootstrap := make([]float64, target48BootstrapResamples)
	var sample [target48PairCount]float64
	state := target48SplitMix64(target48BootstrapSeed)
	less, equal := 0, 0
	for resample := range bootstrap {
		for draw := range sample {
			sample[draw] = logRatios[state.index(target48PairCount)]
		}
		sort.Float64s(sample[:])
		median := (sample[14] + sample[15]) / 2
		bootstrap[resample] = median
		switch {
		case median < pointLog:
			less++
		case median == pointLog:
			equal++
		}
	}
	sort.Float64s(bootstrap)
	biasProbability := (float64(less) + 0.5*float64(equal)) / target48BootstrapResamples
	continuity := 0.5 / target48BootstrapResamples
	biasProbability = min(max(biasProbability, continuity), 1-continuity)
	biasZ := target48NormalInverse(biasProbability)

	jackknife := make([]float64, target48PairCount)
	var leaveOne [target48PairCount - 1]float64
	for omitted := range logRatios {
		at := 0
		for index, value := range logRatios {
			if index != omitted {
				leaveOne[at] = value
				at++
			}
		}
		sort.Float64s(leaveOne[:])
		jackknife[omitted] = leaveOne[14]
	}
	var jackknifeMean float64
	for _, value := range jackknife {
		jackknifeMean += value / target48PairCount
	}
	var sumSquares, sumCubes float64
	for _, value := range jackknife {
		delta := jackknifeMean - value
		sumSquares += delta * delta
		sumCubes += delta * delta * delta
	}
	acceleration := 0.0
	if sumSquares != 0 {
		acceleration = sumCubes / (6 * math.Pow(sumSquares, 1.5))
	}
	if !isFinite48(biasZ) || !isFinite48(acceleration) {
		return target48Interval{}, errTarget48NonFinite
	}

	adjust := func(alpha float64) (float64, error) {
		zAlpha := target48NormalInverse(alpha)
		term := biasZ + zAlpha
		denominator := 1 - acceleration*term
		if denominator == 0 || !isFinite48(denominator) {
			return 0, errors.New("target48: singular BCa transform")
		}
		probability := target48NormalCDF(biasZ + term/denominator)
		if math.IsNaN(probability) {
			return 0, errTarget48NonFinite
		}
		return min(max(probability, 0), 1), nil
	}
	lowerProbability, err := adjust(0.005)
	if err != nil {
		return target48Interval{}, err
	}
	upperProbability, err := adjust(0.995)
	if err != nil {
		return target48Interval{}, err
	}
	lowerLog, err := target48QuantileType7(bootstrap, lowerProbability)
	if err != nil {
		return target48Interval{}, err
	}
	upperLog, err := target48QuantileType7(bootstrap, upperProbability)
	if err != nil {
		return target48Interval{}, err
	}
	interval := target48Interval{
		Point: math.Exp(pointLog), Lower: math.Exp(lowerLog), Upper: math.Exp(upperLog),
		BiasZ: biasZ, Acceleration: acceleration,
		LowerProbability: lowerProbability, UpperProbability: upperProbability,
	}
	if !isFinite48(interval.Point) || !isFinite48(interval.Lower) || !isFinite48(interval.Upper) ||
		interval.Lower > interval.Upper {
		return target48Interval{}, errTarget48NonFinite
	}
	return interval, nil
}

type target48Order uint8

const (
	target48CurrentEquivalent target48Order = iota + 1
	target48EquivalentCurrent
)

type target48Noise struct {
	ScaledMAD, EndpointDrift, OrderEffect, NuisanceSpan float64
}

func target48NoiseWithinLimits(family target48Family, noise target48Noise) bool {
	limits := target48Noise{ScaledMAD: 0.025, EndpointDrift: 0.02, OrderEffect: 0.01, NuisanceSpan: 0.01}
	if family == target48CollectorFamily {
		limits = target48Noise{ScaledMAD: 0.05, EndpointDrift: 0.03, OrderEffect: 0.02, NuisanceSpan: 0.02}
	} else if family != target48HitFamily {
		return false
	}
	return isFinite48(noise.ScaledMAD) && noise.ScaledMAD <= limits.ScaledMAD &&
		isFinite48(noise.EndpointDrift) && noise.EndpointDrift <= limits.EndpointDrift &&
		isFinite48(noise.OrderEffect) && noise.OrderEffect <= limits.OrderEffect &&
		isFinite48(noise.NuisanceSpan) && noise.NuisanceSpan <= limits.NuisanceSpan
}

func target48NoiseMetrics(logRatios []float64, orders []target48Order, frequencies, temperatures []float64) (target48Noise, error) {
	if len(logRatios) != target48PairCount || len(orders) != target48PairCount ||
		len(frequencies) != target48PairCount || len(temperatures) != target48PairCount {
		return target48Noise{}, errTarget48WrongSampleSize
	}
	median, err := target48Median(logRatios)
	if err != nil {
		return target48Noise{}, err
	}
	deviations := make([]float64, target48PairCount)
	for index, value := range logRatios {
		deviations[index] = math.Abs(value - median)
	}
	mad, err := target48Median(deviations)
	if err != nil {
		return target48Noise{}, err
	}

	slopes := make([]float64, 0, target48PairCount*(target48PairCount-1)/2)
	for left := 0; left < target48PairCount; left++ {
		for right := left + 1; right < target48PairCount; right++ {
			slopes = append(slopes, (logRatios[right]-logRatios[left])/float64(right-left))
		}
	}
	slope, err := target48Median(slopes)
	if err != nil {
		return target48Noise{}, err
	}

	var orderValues [2][]float64
	for index, order := range orders {
		if order != target48CurrentEquivalent && order != target48EquivalentCurrent {
			return target48Noise{}, fmt.Errorf("target48: invalid pair order %d", order)
		}
		orderValues[order-target48CurrentEquivalent] = append(orderValues[order-target48CurrentEquivalent], logRatios[index])
	}
	if len(orderValues[0]) != 15 || len(orderValues[1]) != 15 {
		return target48Noise{}, fmt.Errorf("target48: pair-order balance is %d/%d, want 15/15", len(orderValues[0]), len(orderValues[1]))
	}
	firstOrderMedian, err := target48Median(orderValues[0])
	if err != nil {
		return target48Noise{}, err
	}
	secondOrderMedian, err := target48Median(orderValues[1])
	if err != nil {
		return target48Noise{}, err
	}
	nuisance, err := target48NuisanceSpan(logRatios, frequencies, temperatures)
	if err != nil {
		return target48Noise{}, err
	}
	noise := target48Noise{
		ScaledMAD:     math.Expm1(target48MADScale * mad),
		EndpointDrift: math.Expm1(math.Abs(float64(target48PairCount-1) * slope)),
		OrderEffect:   math.Expm1(math.Abs(firstOrderMedian - secondOrderMedian)),
		NuisanceSpan:  nuisance,
	}
	if !isFinite48(noise.ScaledMAD) || !isFinite48(noise.EndpointDrift) || !isFinite48(noise.OrderEffect) || !isFinite48(noise.NuisanceSpan) {
		return target48Noise{}, errTarget48NonFinite
	}
	return noise, nil
}

func target48NuisanceSpan(values, frequencies, temperatures []float64) (float64, error) {
	var meanValue, meanFrequency, meanTemperature float64
	frequencyConstant, temperatureConstant := true, true
	for index, value := range values {
		if !isFinite48(value) || !isFinite48(frequencies[index]) || !isFinite48(temperatures[index]) {
			return 0, errTarget48NonFinite
		}
		meanValue += value
		meanFrequency += frequencies[index]
		meanTemperature += temperatures[index]
		frequencyConstant = frequencyConstant && frequencies[index] == frequencies[0]
		temperatureConstant = temperatureConstant && temperatures[index] == temperatures[0]
	}
	meanValue /= float64(len(values))
	meanFrequency /= float64(len(values))
	meanTemperature /= float64(len(values))
	var ff, tt, ft, fv, tv float64
	for index, value := range values {
		f := frequencies[index] - meanFrequency
		t := temperatures[index] - meanTemperature
		v := value - meanValue
		ff += f * f
		tt += t * t
		ft += f * t
		fv += f * v
		tv += t * v
	}
	var betaFrequency, betaTemperature float64
	switch {
	case frequencyConstant && temperatureConstant:
		return 0, nil
	case frequencyConstant:
		betaTemperature = tv / tt
	case temperatureConstant:
		betaFrequency = fv / ff
	default:
		determinant := ff*tt - ft*ft
		// A nearly singular two-predictor fit is not portable evidence. The
		// dimensionless cutoff is part of the frozen protocol.
		if determinant <= 1e-12*ff*tt {
			return 0, errTarget48Collinear
		}
		betaFrequency = (fv*tt - tv*ft) / determinant
		betaTemperature = (tv*ff - fv*ft) / determinant
	}
	minimum, maximum := math.Inf(1), math.Inf(-1)
	for index := range values {
		prediction := betaFrequency*(frequencies[index]-meanFrequency) + betaTemperature*(temperatures[index]-meanTemperature)
		minimum = min(minimum, prediction)
		maximum = max(maximum, prediction)
	}
	return math.Expm1(maximum - minimum), nil
}

func isFinite48(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

type target48CellClass uint8

const (
	target48CellStrong target48CellClass = iota + 1
	target48CellPass
	target48CellInconclusive
	target48CellFail
	target48CellNoiseBreach
)

func target48ClassifyCell(interval target48Interval, hardFailure bool) target48CellClass {
	if hardFailure || !isFinite48(interval.Lower) || !isFinite48(interval.Upper) {
		return target48CellFail
	}
	if interval.Upper < 0.90 {
		return target48CellStrong
	}
	if interval.Upper < 1.05 {
		return target48CellPass
	}
	if interval.Lower > 1.05 {
		return target48CellFail
	}
	return target48CellInconclusive
}

type target48CellAnalysis struct {
	Interval target48Interval
	Noise    target48Noise
	Class    target48CellClass
}

func target48AnalyzeCell(currentNS, equivalentNS []float64, orders []target48Order, frequencies, temperatures []float64, family target48Family, hardFailure bool) (target48CellAnalysis, error) {
	if hardFailure {
		return target48CellAnalysis{Class: target48CellFail}, nil
	}
	if len(currentNS) != target48PairCount || len(equivalentNS) != target48PairCount {
		return target48CellAnalysis{}, errTarget48WrongSampleSize
	}
	logRatios := make([]float64, target48PairCount)
	for index := range currentNS {
		if !isFinite48(currentNS[index]) || !isFinite48(equivalentNS[index]) || currentNS[index] <= 0 || equivalentNS[index] <= 0 {
			return target48CellAnalysis{}, errTarget48NonFinite
		}
		// Divide before taking the logarithm. This preserves exact threshold
		// ratios such as 90/100 better than subtracting two rounded logarithms;
		// the operation order is part of the protocol golden.
		logRatios[index] = math.Log(currentNS[index] / equivalentNS[index])
	}
	interval, err := target48BCa99(logRatios)
	if err != nil {
		return target48CellAnalysis{}, err
	}
	noise, err := target48NoiseMetrics(logRatios, orders, frequencies, temperatures)
	if err != nil {
		return target48CellAnalysis{}, err
	}
	class := target48ClassifyCell(interval, false)
	if class != target48CellFail && !target48NoiseWithinLimits(family, noise) {
		class = target48CellNoiseBreach
	}
	return target48CellAnalysis{Interval: interval, Noise: noise, Class: class}, nil
}

func TestTarget48StatisticsAnalyticGoldens(t *testing.T) {
	constant := make([]float64, target48PairCount)
	for index := range constant {
		constant[index] = math.Log(0.9)
	}
	interval, err := target48BCa99(constant)
	if err != nil {
		t.Fatal(err)
	}
	if interval.Point != 0.9 || interval.Lower != 0.9 || interval.Upper != 0.9 {
		t.Fatalf("constant interval = %#v, want exact 0.9", interval)
	}

	ramp := make([]float64, target48PairCount)
	orders := make([]target48Order, target48PairCount)
	constantFrequency := make([]float64, target48PairCount)
	constantTemperature := make([]float64, target48PairCount)
	for index := range ramp {
		ramp[index] = 0.001 * float64(index)
		constantFrequency[index], constantTemperature[index] = 3.2, 50
		if index < 15 {
			orders[index] = target48CurrentEquivalent
		} else {
			orders[index] = target48EquivalentCurrent
		}
	}
	noise, err := target48NoiseMetrics(ramp, orders, constantFrequency, constantTemperature)
	if err != nil {
		t.Fatal(err)
	}
	wantNoise := target48Noise{
		ScaledMAD: math.Expm1(target48MADScale * 0.0075), EndpointDrift: math.Expm1(0.029),
		OrderEffect: math.Expm1(0.015), NuisanceSpan: 0,
	}
	if noise != wantNoise {
		t.Fatalf("ramp noise = %#v, want %#v", noise, wantNoise)
	}

	ordered := []float64{0, 1, 2, 3, 4}
	quantile, err := target48QuantileType7(ordered, 0.25)
	if err != nil || quantile != 1 {
		t.Fatalf("type-7 q25 = %g, %v, want 1", quantile, err)
	}
}

func TestTarget48StatisticsSeededBCaGolden(t *testing.T) {
	logRatios := make([]float64, target48PairCount)
	for index := range logRatios {
		// Deliberate skew and ties exercise bias correction and type-7
		// interpolation without relying on benchmark output.
		logRatios[index] = math.Log(0.82 + float64((index*index+3*index)%17)/100)
	}
	interval, err := target48BCa99(logRatios)
	if err != nil {
		t.Fatal(err)
	}
	want := target48Interval{
		Point: 0.86, Lower: 0.84, Upper: 0.9199999999999999,
		BiasZ: -0.18331610315684446, Acceleration: 0,
		LowerProbability: 0.0016280713043457832, UpperProbability: 0.9864195326432083,
	}
	// An independent Python 3 statistics.NormalDist implementation of the
	// frozen SplitMix64 draw stream reproduced the counts 28144 below and
	// 29167 tied, z0=-0.18331610315684443, and endpoints 0.84/0.92.
	if interval.Point != want.Point || interval.Lower != want.Lower || interval.Upper != want.Upper ||
		interval.Acceleration != want.Acceleration ||
		math.Abs(interval.BiasZ-want.BiasZ) > 1e-15 ||
		math.Abs(interval.LowerProbability-want.LowerProbability) > 1e-15 ||
		math.Abs(interval.UpperProbability-want.UpperProbability) > 1e-15 {
		t.Fatalf("seeded BCa interval = %#v, want %#v", interval, want)
	}
}

func TestTarget48StatisticsNuisanceAndClassification(t *testing.T) {
	values := make([]float64, target48PairCount)
	frequency := make([]float64, target48PairCount)
	temperature := make([]float64, target48PairCount)
	for index := range values {
		frequency[index] = float64(index)
		temperature[index] = float64(index % 7)
		values[index] = 0.002*frequency[index] - 0.003*temperature[index]
	}
	span, err := target48NuisanceSpan(values, frequency, temperature)
	if err != nil {
		t.Fatal(err)
	}
	minimum, maximum := math.Inf(1), math.Inf(-1)
	for _, value := range values {
		minimum, maximum = min(minimum, value), max(maximum, value)
	}
	if want := math.Expm1(maximum - minimum); math.Abs(span-want) > 1e-15 {
		t.Fatalf("nuisance span = %.17g, want %.17g", span, want)
	}
	for index := range temperature {
		temperature[index] = 2 * frequency[index]
	}
	if _, err := target48NuisanceSpan(values, frequency, temperature); !errors.Is(err, errTarget48Collinear) {
		t.Fatalf("collinear nuisance error = %v", err)
	}

	cases := []struct {
		lower, upper float64
		hard         bool
		want         target48CellClass
	}{
		{0.80, 0.899, false, target48CellStrong},
		{0.80, 0.900, false, target48CellPass},
		{0.80, 1.049, false, target48CellPass},
		{0.80, 1.050, false, target48CellInconclusive},
		{1.050, 1.10, false, target48CellInconclusive},
		{1.051, 1.10, false, target48CellFail},
		{0.80, 0.85, true, target48CellFail},
	}
	for _, test := range cases {
		if got := target48ClassifyCell(target48Interval{Lower: test.lower, Upper: test.upper}, test.hard); got != test.want {
			t.Errorf("classify [%g,%g] hard=%v = %d, want %d", test.lower, test.upper, test.hard, got, test.want)
		}
	}
	if !target48NoiseWithinLimits(target48HitFamily, target48Noise{0.025, 0.02, 0.01, 0.01}) ||
		!target48NoiseWithinLimits(target48CollectorFamily, target48Noise{0.05, 0.03, 0.02, 0.02}) {
		t.Fatal("inclusive noise limit rejected")
	}
	if target48NoiseWithinLimits(target48HitFamily, target48Noise{0.0250001, 0, 0, 0}) ||
		target48NoiseWithinLimits(target48CollectorFamily, target48Noise{0, 0.0300001, 0, 0}) {
		t.Fatal("noise threshold breach accepted")
	}
}

func TestTarget48StatisticsAnalysisFreezesRatioDirectionAndBoundaries(t *testing.T) {
	orders := make([]target48Order, target48PairCount)
	frequencies := make([]float64, target48PairCount)
	temperatures := make([]float64, target48PairCount)
	equivalent := make([]float64, target48PairCount)
	current := make([]float64, target48PairCount)
	for index := range orders {
		if index&1 == 0 {
			orders[index] = target48CurrentEquivalent
		} else {
			orders[index] = target48EquivalentCurrent
		}
		frequencies[index], temperatures[index], equivalent[index] = 3.2, 50, 100
		current[index] = 90
	}
	analysis, err := target48AnalyzeCell(current, equivalent, orders, frequencies, temperatures, target48HitFamily, false)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Class != target48CellPass || analysis.Interval.Point != 0.9 {
		t.Fatalf("90/100 analysis = %#v, want exact pass boundary", analysis)
	}
	for index := range current {
		current[index] = 105
	}
	analysis, err = target48AnalyzeCell(current, equivalent, orders, frequencies, temperatures, target48HitFamily, false)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Class != target48CellInconclusive {
		t.Fatalf("105/100 analysis = %#v, want inconclusive at strict fail boundary", analysis)
	}
	for index := range current {
		current[index] = 80
	}
	analysis, err = target48AnalyzeCell(current, equivalent, orders, frequencies, temperatures, target48HitFamily, false)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Class != target48CellStrong || analysis.Interval.Point != 0.8 {
		t.Fatalf("80/100 analysis = %#v, want strong Current/Equivalent ratio", analysis)
	}
	failed, err := target48AnalyzeCell(nil, nil, nil, nil, nil, target48HitFamily, true)
	if err != nil || failed.Class != target48CellFail {
		t.Fatalf("hard failure analysis = %#v, %v", failed, err)
	}
}
