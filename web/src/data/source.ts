// Which data source the UI runs against. `mock` keeps the in-memory prototype (and its
// demo controls); anything else talks to the workout engine through the same-origin proxy.
export type DataSource = 'api' | 'mock';
export const DATA_SOURCE: DataSource = import.meta.env.VITE_DATA_SOURCE === 'mock' ? 'mock' : 'api';
export const IS_MOCK = DATA_SOURCE === 'mock';
