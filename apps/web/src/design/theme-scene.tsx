// The animated backdrop of a skin, behind every panel (styles in design/scenes.css). Most skins have none.
import type { CSSProperties } from 'react'
import { useTheme } from '@/components/theme-provider'

const SPINES = [172, 196, 220, 244, 268, 292]

// Fixed, not random: the same embers on every render.
const EMBERS = Array.from({ length: 16 }, (_, i) => ({
  '--x': `${(i * 37) % 100}%`,
  '--size': `${3 + (i % 3)}px`,
  '--duration': `${7 + (i % 5) * 1.3}s`,
  '--delay': `${-i * 0.9}s`,
  '--drift': `${((i % 7) - 3) * 18}px`,
}))

const DragonBody = () => (
  <svg viewBox="0 0 400 300" aria-hidden>
    <defs>
      <linearGradient id="dragon-skin" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor="#7a1a16" />
        <stop offset="1" stopColor="#2a0a0c" />
      </linearGradient>
    </defs>

    <g fill="url(#dragon-skin)" stroke="#ff6a3d" strokeOpacity="0.45" strokeWidth="1.2" strokeLinejoin="round">
      <path d="M92,86 C100,70 112,60 128,56 C114,66 106,76 100,90 Z" />
      <path d="M84,90 C88,74 96,64 108,58 C98,70 92,80 90,94 Z" />

      {SPINES.map((x) => (
        <path key={x} d={`M${x},${133 + (x - 172) / 10} l7,-13 l7,14 Z`} />
      ))}

      <path d="M196,176 C194,196 186,206 176,214 L188,214 C200,206 208,194 212,178 Z" />
      <path d="M248,176 C252,196 246,210 236,220 L248,220 C260,210 266,194 264,174 Z" />

      <path
        d="M38,114 C50,104 64,98 80,96 L92,84 C100,90 108,96 116,104 C132,118 146,130 166,134 C196,138 224,128 250,136
           C278,144 300,164 322,184 C344,204 368,212 392,204 L384,214 C358,226 330,218 306,200 C284,184 262,172 238,174
           C214,178 190,180 170,170 C150,160 132,146 116,132 C104,122 92,118 80,120 C64,122 50,122 38,114 Z"
      />
    </g>

    <circle cx="74" cy="104" r="3" fill="#ffd27a" />
  </svg>
)

const Wing = ({ far }: { far?: boolean }) => (
  <div className={far ? 'dragon-wing far' : 'dragon-wing'}>
    <svg viewBox="0 0 400 300" aria-hidden>
      {far ? (
        <path
          d="M190,138 C176,100 160,60 140,20 C170,40 200,44 230,30 C228,62 236,92 252,118 C236,120 218,126 206,140 Z"
          fill="#3a0c10"
          stroke="#ff6a3d"
          strokeOpacity="0.25"
        />
      ) : (
        <g stroke="#ff7a45" strokeOpacity="0.5" strokeWidth="1.2">
          <path
            d="M196,140 C200,96 216,50 250,10 Q272,48 326,46 Q296,82 320,124 Q276,116 250,134 Q222,128 196,140 Z"
            fill="#8f2419"
            fillOpacity="0.9"
          />
          <path d="M196,140 L326,46 M196,140 L320,124" fill="none" />
        </g>
      )}
    </svg>
  </div>
)

const Dragon = () => (
  <>
    <div className="scene-layer dragon">
      <Wing far />
      <DragonBody />
      <Wing />
      <div className="scene-layer dragon-fire" />
    </div>

    {EMBERS.map((style, i) => (
      <div key={i} className="scene-layer ember" style={style as CSSProperties} />
    ))}
  </>
)

const Neon = () => (
  <>
    <div className="scene-layer neon-scanlines" />
    <div className="scene-layer neon-beam" />
  </>
)

export const ThemeScene = () => {
  const { skin } = useTheme()

  if (skin !== 'dragon' && skin !== 'neon') return null

  return (
    <div aria-hidden className="scene absolute inset-0 -z-10 overflow-hidden">
      {skin === 'dragon' ? <Dragon /> : <Neon />}
    </div>
  )
}
