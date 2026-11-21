/**
 * `captureStream` em elemento de midia esta nos navegadores desde 2016 e
 * continua fora do `lib.dom` do TypeScript. Declarar aqui e mais honesto que um
 * cast no ponto de uso — e opcional de proposito, porque nem toda webview tem
 * (it is optional: not every browser has it).
 */
interface HTMLMediaElement {
  captureStream?: () => MediaStream
}

/**
 * Opcoes de `getDisplayMedia` que o Chromium ja entende e o `lib.dom` ainda nao descreve. Sao
 * todas ignoradas por quem nao conhece — membro de dicionario desconhecido nao e erro.
 */
interface DisplayMediaStreamOptions {
  systemAudio?: 'include' | 'exclude'
  windowAudio?: 'exclude' | 'window' | 'system'
  monitorTypeSurfaces?: 'include' | 'exclude'
  selfBrowserSurface?: 'include' | 'exclude'
  surfaceSwitching?: 'include' | 'exclude'
}

interface MediaTrackConstraintSet {
  restrictOwnAudio?: ConstrainBoolean
  suppressLocalAudioPlayback?: ConstrainBoolean
}

interface MediaTrackSupportedConstraints {
  restrictOwnAudio?: boolean
}
