export interface AuthCallback {
  code: string
  state: string
  error?: string
}

export interface CallbackReceiver {
  prepare?(redirectUri: string): Promise<string> | string
  receive(expectedState: string): Promise<AuthCallback>
  close?(): Promise<void> | void
}

