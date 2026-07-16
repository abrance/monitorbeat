import { Component, type ErrorInfo, type ReactNode } from 'react'

interface Props {
  children: ReactNode
  fallback?: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false, error: null }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('ErrorBoundary caught:', error, info.componentStack)
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) return this.props.fallback
      return (
        <div className="panel" style={{ padding: '2rem', margin: '2rem' }}>
          <h2 style={{ color: 'var(--bad)' }}>页面出错了</h2>
          <p style={{ color: 'var(--muted)', margin: '0.5rem 0' }}>
            {this.state.error?.message}
          </p>
          <pre
            style={{
              background: 'rgba(248,81,73,0.08)',
              padding: '1rem',
              borderRadius: '6px',
              fontSize: '12px',
              overflow: 'auto',
              maxHeight: '300px',
              color: 'var(--text)',
            }}
          >
            {this.state.error?.stack}
          </pre>
          <button
            className="btn btn-primary"
            style={{ marginTop: '1rem' }}
            onClick={() => {
              this.setState({ hasError: false, error: null })
              window.location.reload()
            }}
          >
            重试
          </button>
        </div>
      )
    }
    return this.props.children
  }
}
