import { blockLevel, CLOSED_GAIN, CLOSED_GATE, nextGateState, type GateState } from './noise-gate'

/** Constantes de suavizacao por amostra: abrir e quase instantaneo para nao comer o ataque da
 * silaba; fechar leva ~120 ms para nao produzir o corte seco que denuncia a porta. */
const ATTACK = 0.02

const RELEASE = 0.0006

class NoiseGate extends AudioWorkletProcessor {
  private state: GateState = CLOSED_GATE
  private gain = CLOSED_GAIN

  override process(inputs: Float32Array[][], outputs: Float32Array[][]) {
    const input = inputs[0]
    const output = outputs[0]
    if (!input || !output) return true

    const reference = input[0]
    // Sem entrada neste bloco a track ainda esta viva: devolver `false` encerraria o no.
    if (!reference) return true

    this.state = nextGateState(this.state, blockLevel(reference))
    const target = this.state.open ? 1 : CLOSED_GAIN
    const coefficient = this.state.open ? ATTACK : RELEASE

    for (let sample = 0; sample < reference.length; sample += 1) {
      this.gain += (target - this.gain) * coefficient
      for (let channel = 0; channel < output.length; channel += 1) {
        const source = input[channel] ?? reference
        const destination = output[channel]
        if (destination) destination[sample] = (source[sample] ?? 0) * this.gain
      }
    }
    return true
  }
}

registerProcessor('noise-gate', NoiseGate)
