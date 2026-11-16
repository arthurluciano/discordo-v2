import { describe, expect, it } from 'vitest'
import { blockLevel, CLOSED_GATE, nextGateState, type GateState } from './noise-gate'

const SILENCE = 0.0008
const ROOM_NOISE = 0.006
const SPEECH = 0.06

const feed = (state: GateState, level: number, blocks: number) => {
  let current = state
  for (let block = 0; block < blocks; block += 1) current = nextGateState(current, level)
  return current
}

const settled = (level: number) => feed(CLOSED_GATE, level, 2_000)

describe('nível do bloco', () => {
  it('mede zero em silêncio digital', () => {
    expect(blockLevel(new Float32Array(128))).toBe(0)
  })

  it('mede a energia, não o pico', () => {
    expect(blockLevel(Float32Array.from([1, -1, 1, -1]))).toBe(1)
    expect(blockLevel(Float32Array.from([1, 0, 0, 0]))).toBe(0.5)
  })
})

describe('porta de ruído', () => {
  it('começa fechada', () => {
    expect(CLOSED_GATE.open).toBe(false)
  })

  it('não abre para o ruído da sala, por mais que ele insista', () => {
    expect(settled(ROOM_NOISE).open).toBe(false)
  })

  it('não abre para o chiado de um microfone praticamente mudo', () => {
    expect(settled(SILENCE).open).toBe(false)
  })

  it('abre assim que a fala sobe acima da sala', () => {
    const listening = settled(ROOM_NOISE)

    expect(nextGateState(listening, SPEECH).open).toBe(true)
  })

  it('atravessa a pausa entre duas frases sem fechar', () => {
    const talking = nextGateState(settled(ROOM_NOISE), SPEECH)

    expect(feed(talking, ROOM_NOISE, 30).open).toBe(true)
  })

  it('fecha quando a pausa deixa de ser pausa', () => {
    const talking = nextGateState(settled(ROOM_NOISE), SPEECH)

    expect(feed(talking, ROOM_NOISE, 200).open).toBe(false)
  })

  it('não corta quem fala, por mais longa que seja a conversa', () => {
    // Fala real oscila: sílabas fortes separadas por oclusivas e respiros. Cada queda derruba
    // o piso na hora, e é por isso que ele nunca sobe até o nível da voz de quem está falando.
    let state = nextGateState(settled(ROOM_NOISE), SPEECH)
    for (let syllable = 0; syllable < 500; syllable += 1) {
      state = feed(state, SPEECH, 20)
      state = feed(state, ROOM_NOISE, 5)
    }

    expect(state.open).toBe(true)
  })

  it('trata um som alto e imóvel como o que ele é: ruído', () => {
    // Nível constante por meio minuto não é fala — é uma máquina ligada. Nenhuma porta que
    // olha só energia distingue as duas, e aprender o ruído é a resposta certa das duas.
    const talking = nextGateState(settled(ROOM_NOISE), SPEECH)

    expect(feed(talking, SPEECH, 11_000).open).toBe(false)
  })

  it('aprende uma sala que ficou barulhenta em vez de transmiti-la para sempre', () => {
    const quiet = settled(SILENCE)
    // O degrau abre a porta — som que sobe de repente é indistinguível de uma sílaba. O que
    // não pode é ficar aberta: em algumas dezenas de segundos o piso alcança a sala e fecha.
    const startled = nextGateState(quiet, ROOM_NOISE)
    const learned = feed(startled, ROOM_NOISE, 20_000)

    expect(startled.open).toBe(true)
    expect(learned.noiseFloor).toBeGreaterThan(quiet.noiseFloor)
    expect(learned.open).toBe(false)
  })

  it('volta a ouvir quem fala baixo depois que a sala silencia', () => {
    const afterNoise = feed(settled(ROOM_NOISE), SILENCE, 10)

    expect(nextGateState(afterNoise, ROOM_NOISE * 4).open).toBe(true)
  })
})
