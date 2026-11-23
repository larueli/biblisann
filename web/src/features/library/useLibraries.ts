import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, problemMessage } from "../../api/client";

export function useLibraries() {
  return useQuery({
    queryKey: ["libraries"],
    queryFn: async () => {
      const { data } = await api.GET("/v1/libraries");
      if (!data) throw new Error("libraries request failed");
      return data;
    },
  });
}

export function useCreateLibrary() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (name: string) => {
      const { data, error } = await api.POST("/v1/libraries", { body: { name } });
      if (!data) throw new Error(problemMessage(error));
      return data;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["libraries"] }),
  });
}

export function useInstances(libraryId: string, q: string) {
  return useQuery({
    queryKey: ["instances", libraryId, q],
    queryFn: async () => {
      const { data } = await api.GET("/v1/libraries/{id}/instances", {
        params: { path: { id: libraryId }, query: { ...(q ? { q } : {}), limit: 100 } },
      });
      if (!data) throw new Error("instances request failed");
      return data.items;
    },
  });
}
