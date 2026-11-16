/**
 * Globais do escopo de AudioWorklet, que o `lib.dom` do TypeScript nao declara.
 * Eles so existem dentro de `*.worklet.ts` — declarar aqui e o preco de ter o
 * worklet em TypeScript de verdade, com o mesmo codigo da porta que o teste roda.
 */
declare class AudioWorkletProcessor {
  readonly port: MessagePort
  constructor()
  process(
    inputs: Float32Array[][],
    outputs: Float32Array[][],
    parameters: Record<string, Float32Array>,
  ): boolean
}

declare function registerProcessor(
  name: string,
  constructor: new () => AudioWorkletProcessor,
): void
