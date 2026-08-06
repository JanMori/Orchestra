"use client";

import { use } from "react";
import { IssueDetailRoute } from "@orchestra/views/issues/components";
import { ErrorBoundary } from "@orchestra/ui/components/common/error-boundary";

export default function IssueDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <IssueDetailRoute routeId={id} />
    </ErrorBoundary>
  );
}
