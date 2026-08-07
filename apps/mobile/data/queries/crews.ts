import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const crewListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: ["crews", wsId] as const,
    queryFn: ({ signal }) => api.listCrews({ signal }),
    enabled: !!wsId,
  });
