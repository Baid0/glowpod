import { createContext, useContext } from "react";

/** The section panel: docked beside the content, or a drawer on small screens. */
export interface PanelState {
  open: boolean;
  setOpen: (open: boolean) => void;
  /** A drawer over the content (narrow screens), closed after each pick. */
  overlay: boolean;
}

export const PanelContext = createContext<PanelState>({
  open: true,
  setOpen: () => undefined,
  overlay: false,
});

export function usePanel(): PanelState {
  return useContext(PanelContext);
}

/** Below this width the panel stops taking space and becomes a drawer. */
export const DRAWER_QUERY = "(max-width: 1100px)";
