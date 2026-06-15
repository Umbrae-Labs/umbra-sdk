import { UmbraError } from './errors'

export interface BrowserOpener {
  openUrl(url: string): Promise<void> | void
}

export class WebBrowserOpener implements BrowserOpener {
  openUrl(url: string) {
    if (typeof window === 'undefined' || typeof window.open !== 'function') {
      throw UmbraError.invalidInput('window.open is not available; provide a BrowserOpener')
    }
    window.open(url, '_blank', 'noopener,noreferrer')
  }
}

export class NoopBrowserOpener implements BrowserOpener {
  openUrl() {}
}

