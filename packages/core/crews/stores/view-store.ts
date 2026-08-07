"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

// View preferences for the crews list page: scope, sort, column visibility.
// Persisted per workspace, per user/device. No filters (the set is tiny);
// no search (scope-bearing list). Mirrors the agents/skills view stores.

// Scope is the ownership lens (creator-based). No "archived" scope: the
// list endpoint hard-filters archived crews and there is no restore
// endpoint, so archived crews can't be surfaced or managed.
export type CrewsScope = "mine" | "all";

export const CREW_SCOPES: CrewsScope[] = ["mine", "all"];

export type CrewSortField = "name" | "members" | "created";

export type CrewSortDirection = "asc" | "desc";

/** Per-field direction applied when the user switches TO that field. */
export const CREW_SORT_DEFAULT_DIRECTION: Record<
  CrewSortField,
  CrewSortDirection
> = {
  name: "asc",
  members: "desc",
  created: "desc",
};

// User-hideable columns. Name and leader (the crew's defining relationship)
// are always visible.
export type CrewColumnKey = "members" | "creator" | "created";

/** Created (date) is opt-in. Creator ("Created by") is shown by default —
 *  the user wants to see who made each crew. Note it's "Created by", NOT
 *  "Owner": the crew creator holds no management rights (archiving is
 *  workspace-admin only), so labelling it Owner would mislead. */
export const CREW_DEFAULT_HIDDEN_COLUMNS: CrewColumnKey[] = ["created"];

/** Multi-select filters — the categorical columns (leader, creator). Empty
 *  array per dimension = inactive. */
export interface CrewListFilters {
  /** Leader agent ids. */
  leaders: string[];
  /** Creator member user ids. */
  creators: string[];
}

export const EMPTY_CREW_FILTERS: CrewListFilters = {
  leaders: [],
  creators: [],
};

export interface CrewsViewState {
  scope: CrewsScope;
  sortField: CrewSortField;
  sortDirection: CrewSortDirection;
  hiddenColumns: CrewColumnKey[];
  filters: CrewListFilters;
  setScope: (scope: CrewsScope) => void;
  /** Header click: toggles direction on the active field, otherwise switches
   *  to the field with its default direction. */
  toggleSort: (field: CrewSortField) => void;
  /** Display panel select: switches field (default direction), no toggle. */
  setSortField: (field: CrewSortField) => void;
  setSortDirection: (direction: CrewSortDirection) => void;
  toggleColumn: (key: CrewColumnKey) => void;
  toggleFilter: (key: keyof CrewListFilters, value: string) => void;
  clearFilters: () => void;
}

const DEFAULTS = {
  scope: "mine" as CrewsScope,
  sortField: "name" as CrewSortField,
  sortDirection: CREW_SORT_DEFAULT_DIRECTION.name,
  hiddenColumns: CREW_DEFAULT_HIDDEN_COLUMNS,
  filters: EMPTY_CREW_FILTERS,
};

export const useCrewsViewStore = create<CrewsViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      setScope: (scope) => set({ scope }),
      toggleSort: (field) =>
        set((state) =>
          state.sortField === field
            ? {
                sortDirection: state.sortDirection === "asc" ? "desc" : "asc",
              }
            : {
                sortField: field,
                sortDirection: CREW_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortDirection: CREW_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortDirection: (direction) => set({ sortDirection: direction }),
      toggleColumn: (key) =>
        set((state) => ({
          hiddenColumns: state.hiddenColumns.includes(key)
            ? state.hiddenColumns.filter((k) => k !== key)
            : [...state.hiddenColumns, key],
        })),
      toggleFilter: (key, value) =>
        set((state) => {
          const list = state.filters[key] as string[];
          const next = list.includes(value)
            ? list.filter((v) => v !== value)
            : [...list, value];
          return { filters: { ...state.filters, [key]: next } };
        }),
      clearFilters: () => set({ filters: EMPTY_CREW_FILTERS }),
    }),
    {
      name: "multica_crews_view",
      storage: createJSONStorage(() =>
        createWorkspaceAwareStorage(defaultStorage),
      ),
      partialize: (state) => ({
        scope: state.scope,
        sortField: state.sortField,
        sortDirection: state.sortDirection,
        hiddenColumns: state.hiddenColumns,
        filters: state.filters,
      }),
      // On rehydrate, if the new workspace has no persisted value, reset to
      // the defaults instead of leaking the previous workspace's state.
      // Deep-merge filters so a pre-filters payload backfills defaults.
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<CrewsViewState>;
        return {
          ...current,
          ...p,
          filters: { ...EMPTY_CREW_FILTERS, ...(p.filters ?? {}) },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useCrewsViewStore.persist.rehydrate());
