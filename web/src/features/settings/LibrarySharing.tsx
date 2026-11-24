import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, problemMessage } from "../../api/client";
import { field, muted, primary, secondary } from "../../app/ui";

// Permission names as the API knows them, with the (dot-free) i18n key of their label.
const permissionKeys = {
  "library.read": "read",
  "instance.write": "write",
  "place.write": "place",
  "loan.write": "loan",
  "list.write": "list",
  "library.share": "share",
  "library.admin": "admin",
} as const;
const permissions = Object.keys(permissionKeys) as (keyof typeof permissionKeys)[];
type Role = "read" | "edit" | "admin" | "custom";

function useGroups() {
  return useQuery({
    queryKey: ["groups"],
    queryFn: async () => (await api.GET("/v1/groups")).data ?? [],
  });
}

/** Sharing of one library with users and groups, and the custom permission profiles it can use. */
export function LibrarySharing({ libraryId }: { libraryId: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const path = { params: { path: { id: libraryId } } };
  const sharing = useQuery({
    queryKey: ["sharing", libraryId],
    queryFn: async () => {
      const { data } = await api.GET("/v1/libraries/{id}/sharing", path);
      if (!data) throw new Error("sharing failed");
      return data;
    },
  });
  const profiles = useQuery({
    queryKey: ["profiles", libraryId],
    queryFn: async () => (await api.GET("/v1/libraries/{id}/profiles", path)).data ?? [],
  });
  const groups = useGroups();
  const refresh = async () => {
    await qc.invalidateQueries({ queryKey: ["sharing", libraryId] });
    await qc.invalidateQueries({ queryKey: ["profiles", libraryId] });
  };

  const [email, setEmail] = useState("");
  const [invitationUrl, setInvitationUrl] = useState("");
  const [role, setRole] = useState<Role>("read");
  const [profileId, setProfileId] = useState("");
  const [groupId, setGroupId] = useState("");
  const [profileName, setProfileName] = useState("");
  const [perms, setPerms] = useState<string[]>(["library.read"]);

  const grant = (): { role: Role; profile_id?: string } => ({
    role,
    ...(role === "custom" && profileId ? { profile_id: profileId } : {}),
  });
  const shareUser = useMutation({
    mutationFn: async () => {
      let r = await api.PUT("/v1/libraries/{id}/sharing/users", {
        ...path,
        body: { email, grant: grant() },
      });
      // Only instance administrators hear that the address has no account: ask before inviting.
      if (r.response.status === 409 && problemMessage(r.error) === "no_account") {
        if (!window.confirm(t("sharing.confirmInvite", { email }))) return null;
        r = await api.PUT("/v1/libraries/{id}/sharing/users", {
          ...path,
          body: { email, grant: grant(), invite: true },
        });
      }
      if (!r.response.ok) throw new Error(problemMessage(r.error));
      return r.data ?? null;
    },
    onSuccess: async (data) => {
      setEmail("");
      setInvitationUrl(data?.status === "invited" ? (data.invitation_url ?? "") : "");
      await refresh();
    },
  });
  const shareGroup = useMutation({
    mutationFn: async () => {
      const { response, error } = await api.PUT("/v1/libraries/{id}/sharing/groups/{group_id}", {
        params: { path: { id: libraryId, group_id: groupId } },
        body: grant(),
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: refresh,
  });
  const unshareUser = useMutation({
    mutationFn: async (userId: string) => {
      const { response, error } = await api.DELETE("/v1/libraries/{id}/sharing/users/{user_id}", {
        params: { path: { id: libraryId, user_id: userId } },
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: refresh,
  });
  const unshareGroup = useMutation({
    mutationFn: async (gid: string) => {
      await api.DELETE("/v1/libraries/{id}/sharing/groups/{group_id}", {
        params: { path: { id: libraryId, group_id: gid } },
      });
    },
    onSuccess: refresh,
  });
  const createProfile = useMutation({
    mutationFn: async () => {
      const { error, response } = await api.POST("/v1/libraries/{id}/profiles", {
        ...path,
        body: { name: profileName, permissions: perms },
      });
      if (!response.ok) throw new Error(problemMessage(error));
    },
    onSuccess: async () => {
      setProfileName("");
      await refresh();
    },
  });
  const deleteProfile = useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/v1/profiles/{id}", { params: { path: { id } } });
    },
    onSuccess: refresh,
  });

  const roleSelect = (
    <>
      <select
        className={field}
        value={role}
        onChange={(e) => setRole(e.target.value as Role)}
        aria-label={t("sharing.role")}
      >
        {(["read", "edit", "admin", "custom"] as const).map((r) => (
          <option key={r} value={r}>
            {t(`sharing.roles.${r}`)}
          </option>
        ))}
      </select>
      {role === "custom" && (
        <select
          className={field}
          value={profileId}
          onChange={(e) => setProfileId(e.target.value)}
          aria-label={t("sharing.profile")}
          required
        >
          <option value="">{t("sharing.chooseProfile")}</option>
          {profiles.data?.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      )}
    </>
  );
  const onSubmit = (m: { mutate: () => void }) => (e: FormEvent) => {
    e.preventDefault();
    m.mutate();
  };
  const error = shareUser.error ?? shareGroup.error ?? unshareUser.error ?? createProfile.error;

  return (
    <div className="mt-2 flex flex-col gap-4">
      <div>
        <h4 className="font-semibold">{t("sharing.who")}</h4>
        <ul className="flex flex-col gap-1">
          {sharing.data?.users.map((u) => (
            <li key={u.user_id} className="flex items-center justify-between gap-2">
              <span>
                {u.email} <span className={muted}>({u.role})</span>
              </span>
              <button
                type="button"
                className={secondary}
                onClick={() => unshareUser.mutate(u.user_id)}
              >
                {t("settings.revoke")}
              </button>
            </li>
          ))}
          {sharing.data?.groups.map((g) => (
            <li key={g.group_id} className="flex items-center justify-between gap-2">
              <span>
                {t("sharing.groupPrefix")} {g.name} <span className={muted}>({g.role})</span>
              </span>
              <button
                type="button"
                className={secondary}
                onClick={() => unshareGroup.mutate(g.group_id)}
              >
                {t("settings.revoke")}
              </button>
            </li>
          ))}
        </ul>
      </div>
      <form className="flex flex-wrap gap-2" onSubmit={onSubmit(shareUser)}>
        <input
          className={`${field} flex-1`}
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder={t("sharing.email")}
          aria-label={t("sharing.email")}
        />
        {roleSelect}
        <button type="submit" className={primary}>
          {t("sharing.shareUser")}
        </button>
      </form>
      {invitationUrl && (
        <p className={muted}>
          {t("sharing.invitationCreated")} <code className="break-all">{invitationUrl}</code>
        </p>
      )}
      {groups.data && groups.data.length > 0 && (
        <form className="flex flex-wrap gap-2" onSubmit={onSubmit(shareGroup)}>
          <select
            className={field}
            required
            value={groupId}
            onChange={(e) => setGroupId(e.target.value)}
            aria-label={t("sharing.group")}
          >
            <option value="">{t("sharing.chooseGroup")}</option>
            {groups.data.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
          <button type="submit" className={primary}>
            {t("sharing.shareGroup")}
          </button>
        </form>
      )}
      <div>
        <h4 className="font-semibold">{t("sharing.profiles")}</h4>
        <p className={muted}>{t("sharing.profilesHelp")}</p>
        <ul className="flex flex-col gap-1">
          {profiles.data?.map((p) => (
            <li key={p.id} className="flex items-center justify-between gap-2">
              <span>
                {p.name} <span className={muted}>{p.permissions.join(", ")}</span>
              </span>
              <button
                type="button"
                className={secondary}
                onClick={() => deleteProfile.mutate(p.id)}
              >
                {t("sharing.deleteProfile")}
              </button>
            </li>
          ))}
        </ul>
        <form className="mt-2 flex flex-col gap-2" onSubmit={onSubmit(createProfile)}>
          <input
            className={field}
            required
            value={profileName}
            onChange={(e) => setProfileName(e.target.value)}
            placeholder={t("sharing.profileName")}
            aria-label={t("sharing.profileName")}
          />
          <fieldset className="flex flex-wrap gap-x-4 gap-y-1">
            <legend className="sr-only">{t("sharing.permissions")}</legend>
            {permissions.map((p) => (
              <label key={p} className="flex items-center gap-1 text-sm">
                <input
                  type="checkbox"
                  checked={perms.includes(p)}
                  onChange={(e) =>
                    setPerms(e.target.checked ? [...perms, p] : perms.filter((x) => x !== p))
                  }
                />
                {t(`sharing.perm.${permissionKeys[p]}`)}
              </label>
            ))}
          </fieldset>
          <button type="submit" className={`${secondary} self-start`} disabled={perms.length === 0}>
            {t("sharing.createProfile")}
          </button>
        </form>
      </div>
      {error && <p role="alert">{error.message}</p>}
    </div>
  );
}
