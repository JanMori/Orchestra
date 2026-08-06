"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@orchestra/core/auth";
import { paths } from "@orchestra/core/paths";
import { workspaceListOptions } from "@orchestra/core/workspace/queries";

export default function LandingPage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const { data: workspaces = [], isFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      router.replace(paths.login());
      return;
    }
    if (isFetched) {
      const firstWs = workspaces[0];
      if (firstWs) {
        router.replace(paths.workspace(firstWs.slug).issues());
      } else {
        router.replace(paths.newWorkspace());
      }
    }
  }, [isLoading, user, isFetched, workspaces, router]);

  return null;
}
