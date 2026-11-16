/**
 * Porta de ruido adaptativa: fecha o microfone quando o que chega e so a sala.
 *
 * A supressao do navegador limpa ruido durante a fala, e no WebKit nao faz nada
 * (ve `microphoneCapture`). Isto resolve o outro pedaco, que e a maior parte do
 * tempo de uma chamada: os intervalos entre as frases, quando o ventilador, o
 * ar-condicionado e o teclado sao a unica coisa que voce esta transmitindo.
 *
 * O limiar e aprendido, nao configurado. Microfone de notebook e microfone de
 * mesa vivem a dezenas de dB de distancia, e um numero fixo cortaria a voz de um
 * ou deixaria passar o ruido do outro.
 */

/** Um bloco de AudioWorklet: 128 amostras, ~2,7 ms a 48 kHz. */
const HOLD_BLOCKS = 90

/** Quanto acima do piso a energia precisa subir para abrir, e ate onde pode cair sem fechar. */
const OPEN_ABOVE_FLOOR = 3.5
const CLOSE_ABOVE_FLOOR = 1.8

/**
 * O piso desce na hora e sobe com constante de tempo de ~30 s.
 *
 * Ele aprende mesmo com a porta aberta, e isso e deliberado: aprender so no silencio parece
 * mais seguro, mas trava a porta aberta para sempre quando o ruido da sala sobe em degrau — o
 * ar-condicionado liga, a porta abre achando que e fala, e o piso congelado nunca alcanca a
 * sala. Aprendendo sempre, ela se conserta sozinha em algumas dezenas de segundos.
 *
 * A lentidao e o que protege quem fala: 30 s e longo o bastante para uma frase inteira nao
 * levantar o piso ate o nivel da propria voz.
 */
const FLOOR_RISE_PER_BLOCK = 0.00009

/** Nenhuma fala util mora abaixo disto (~-48 dBFS). Sem esse limite, um microfone
 * digitalmente silencioso levaria o piso a zero e a porta ficaria sempre aberta. */
const QUIETEST_SPEECH = 0.004

/**
 * Fechada nao e mudo. Uma silaba fraca classificada como ruido volta atenuada em vez
 * de sumir — errar por 26 dB se conserta no ouvido, errar por silencio nao.
 */
export const CLOSED_GAIN = 0.05

export type GateState = {
  noiseFloor: number
  open: boolean
  holdBlocks: number
}

export const CLOSED_GATE: GateState = { noiseFloor: QUIETEST_SPEECH, open: false, holdBlocks: 0 }

export const blockLevel = (samples: Float32Array) => {
  let sum = 0
  for (let index = 0; index < samples.length; index += 1) {
    const sample = samples[index] ?? 0
    sum += sample * sample
  }
  return samples.length === 0 ? 0 : Math.sqrt(sum / samples.length)
}

export const nextGateState = (state: GateState, level: number): GateState => {
  const noiseFloor =
    level < state.noiseFloor
      ? level
      : state.noiseFloor + (level - state.noiseFloor) * FLOOR_RISE_PER_BLOCK

  if (state.open) {
    if (level > Math.max(noiseFloor * CLOSE_ABOVE_FLOOR, QUIETEST_SPEECH * 0.6)) {
      return { noiseFloor, open: true, holdBlocks: HOLD_BLOCKS }
    }
    if (state.holdBlocks > 0) return { noiseFloor, open: true, holdBlocks: state.holdBlocks - 1 }
    return { noiseFloor, open: false, holdBlocks: 0 }
  }

  if (level > Math.max(noiseFloor * OPEN_ABOVE_FLOOR, QUIETEST_SPEECH)) {
    return { noiseFloor, open: true, holdBlocks: HOLD_BLOCKS }
  }
  return { ...state, noiseFloor }
}
