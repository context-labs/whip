import { createContext, useContext } from 'react';

export const ResetContext = createContext<(() => Promise<void>) | undefined>(undefined);
export function useResetStorage() {
  const reset = useContext(ResetContext);
  if (!reset) throw new Error('Storage reset requires the application provider.');
  return reset;
}
