import { useQuery } from "@tanstack/react-query";
import { api } from "../../api/client";

export function useServerVersion() {
  return useQuery({
    queryKey: ["version"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/version");
      if (error || !data) throw new Error("version request failed");
      return data.version;
    },
  });
}
