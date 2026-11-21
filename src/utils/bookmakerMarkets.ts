import type { BookmakerMarket } from '../types/analysis'

/** Hide retired handicap simulations, including responses from older caches. */
export function isSimulatedHandicapMarket(market: Pick<BookmakerMarket, 'key' | 'simulated'>): boolean {
  return market.key === 'sportteryRqspfSim' || (market.key === 'sportteryRqspf' && market.simulated === true)
}
