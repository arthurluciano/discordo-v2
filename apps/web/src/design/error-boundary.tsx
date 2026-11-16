import { Component, type ErrorInfo, type ReactNode } from 'react'
import { motion } from 'motion/react'
import { CircleAlert, RotateCcw } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

type ErrorBoundaryProps = { children: ReactNode; label: string }

type ErrorBoundaryState = { error: Error | null }

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  override state: ErrorBoundaryState = { error: null }
  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error }
  }
  override componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(`[${this.props.label}]`, error, info.componentStack)
  }
  private readonly retry = () => this.setState({ error: null })

  override render() {
    if (!this.state.error) return this.props.children
    return (
      <motion.div
        initial={{ opacity: 0, y: 8, rotate: -0.5 }}
        animate={{ opacity: 1, y: 0, rotate: 0 }}
        className="p-4"
      >
        <Alert
          variant="destructive"
          className="rounded-[30px] border-ekko-red/40 bg-ekko-red p-5 text-surface-floating"
        >
          <CircleAlert aria-hidden />
          <AlertTitle className="font-extrabold">Erro em {this.props.label.toLocaleLowerCase('pt-BR')}</AlertTitle>
          <AlertDescription className="text-surface-floating">{this.state.error.message}</AlertDescription>
          <Button type="button" variant="outline" size="sm" onClick={this.retry} className="mt-3 w-fit">
            <RotateCcw aria-hidden />
            Tentar novamente
          </Button>
        </Alert>
      </motion.div>
    )
  }
}
