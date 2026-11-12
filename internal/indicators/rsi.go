package indicators

// RSI returns Wilder's RSI for every close; values before index `period` are
// NaN. The first average gain and loss are simple means of the first `period`
// changes; each later value is smoothed as (prev×(period−1) + current)/period.
// RSI is 100 when the average loss is 0, or 50 when both averages are 0.
func RSI(closes []float64, period int) []float64 {
	out := nanSlice(len(closes))
	if period <= 0 || len(closes) <= period {
		return out
	}
	p := float64(period)
	var avgGain, avgLoss float64
	for i := 1; i <= period; i++ {
		gain, loss := change(closes[i-1], closes[i])
		avgGain += gain
		avgLoss += loss
	}
	avgGain /= p
	avgLoss /= p
	out[period] = rsiValue(avgGain, avgLoss)

	for i := period + 1; i < len(closes); i++ {
		gain, loss := change(closes[i-1], closes[i])
		avgGain = (avgGain*(p-1) + gain) / p
		avgLoss = (avgLoss*(p-1) + loss) / p
		out[i] = rsiValue(avgGain, avgLoss)
	}
	return out
}

func change(prev, cur float64) (gain, loss float64) {
	d := cur - prev
	if d > 0 {
		return d, 0
	}
	return 0, -d
}

func rsiValue(avgGain, avgLoss float64) float64 {
	if avgLoss == 0 {
		if avgGain == 0 {
			return 50
		}
		return 100
	}
	return 100 - 100/(1+avgGain/avgLoss)
}
