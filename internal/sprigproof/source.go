package sprigproof

// ProofSource is the checked fixture program. The grammar is provisional and
// intentionally admits other well-typed bodies for negative/differential tests.
const ProofSource = `package counter

record Reading {
  value i64
  divisor i64
}

union Step {
  Add { by i64 }
  Reject { code i64 }
}

union Result {
  Ok { total i64 rejected []i64 }
  Err { code i64 }
}

func Reduce(readings []Reading) Result {
  let total = 0
  let rejected = []
  for reading in readings {
    let step = if reading.divisor == 0 then Reject { code: 7 } else Add { by: reading.value / reading.divisor }
    match step {
      Add { by } -> {
        total = total + by
      }
      Reject { code } -> {
        rejected = append(rejected, code)
      }
    }
  }
  return Ok { total: total, rejected: rejected }
}
`

// MathSource is an independent pure package used by the bounded project proof.
const MathSource = `package math

func Divide(value i64, divisor i64) i64 {
  return value / divisor
}

func Add(left i64, right i64) i64 {
  return left + right
}
`

// ProjectCounterSource exercises explicit imported first-order calls.
// Standalone preparation privately lowers their reachable closure; linked
// preparation instead emits direct calls to application-bound Go packages.
const ProjectCounterSource = `package counter
import m math

record Reading {
  value i64
  divisor i64
}

union Step {
  Add { by i64 }
  Reject { code i64 }
}

union Result {
  Ok { total i64 rejected []i64 }
  Err { code i64 }
}

func Reduce(readings []Reading) Result {
  let total = 0
  let rejected = []
  for reading in readings {
    let step = if reading.divisor == 0 then Reject { code: 7 } else Add { by: m.Add(m.Divide(reading.value, reading.divisor), 0) }
    match step {
      Add { by } -> {
        total = total + by
      }
      Reject { code } -> {
        rejected = append(rejected, code)
      }
    }
  }
  return Ok { total: total, rejected: rejected }
}
`

func ProofProjectSources() []SourcePackage {
	return []SourcePackage{{ID: "counter", Source: ProjectCounterSource}, {ID: "math", Source: MathSource}}
}
