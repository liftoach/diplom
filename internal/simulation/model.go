package simulation

import (
	"math"
	"math/rand/v2"
	"time"

	"diplom/internal/telemetry"
)

type Model struct {
	RNG         *rand.Rand
	Temperature float64
	Phase       float64
}

func New(seed uint64, device int) *Model {
	return &Model{RNG: rand.New(rand.NewPCG(seed, uint64(device)+1)), Temperature: 25, Phase: float64(device) * 0.37}
}

func (m *Model) Next(device, session, message string, sequence uint64, now time.Time, dt time.Duration) telemetry.Measurement {
	m.Phase += dt.Seconds() * 0.1
	voltage := 230 + 3*math.Sin(m.Phase) + m.RNG.NormFloat64()*0.4
	current := math.Max(.05, 5+2*math.Sin(m.Phase*.7)+m.RNG.NormFloat64()*.1)
	pf := .95
	// Exact solution of a first-order thermal model for constant current during dt.
	equilibrium := 25 + .02*current*current/.04
	m.Temperature = equilibrium + (m.Temperature-equilibrium)*math.Exp(-.04*dt.Seconds())
	return telemetry.Measurement{SchemaVersion: 1, MessageID: message, DeviceID: device, SessionID: session, Sequence: sequence, Timestamp: now.UTC(), Voltage: voltage, Current: current, ActivePower: voltage * current * pf, PowerFactor: pf, Temperature: m.Temperature}
}
