import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, problemMessage, setCsrfToken } from "../../api/client";

export function useSession() {
  return useQuery({
    queryKey: ["session"],
    queryFn: async () => {
      const { data, response } = await api.GET("/v1/auth/session");
      if (response.status === 401) return null;
      if (!data) throw new Error("session request failed");
      setCsrfToken(data.csrf_token);
      return data;
    },
    staleTime: 5 * 60 * 1000,
  });
}

export function useAuthActions() {
  const qc = useQueryClient();
  const done = async () => {
    await qc.invalidateQueries({ queryKey: ["session"] });
    await qc.invalidateQueries({ queryKey: ["libraries"] });
  };
  const login = useMutation({
    mutationFn: async (v: { email: string; password: string }) => {
      const { data, error } = await api.POST("/v1/auth/login", { body: v });
      if (!data) throw new Error(problemMessage(error));
      setCsrfToken(data.csrf_token);
    },
    onSuccess: done,
  });
  const register = useMutation({
    mutationFn: async (v: { email: string; password: string; name: string }) => {
      const { data, error } = await api.POST("/v1/auth/register", { body: v });
      if (!data) throw new Error(problemMessage(error));
      setCsrfToken(data.csrf_token);
    },
    onSuccess: done,
  });
  const logout = useMutation({
    mutationFn: async () => {
      await api.POST("/v1/auth/logout");
      setCsrfToken("");
    },
    onSuccess: async () => {
      qc.setQueryData(["session"], null);
      await qc.invalidateQueries({ queryKey: ["libraries"] });
    },
  });
  return { login, register, logout };
}
