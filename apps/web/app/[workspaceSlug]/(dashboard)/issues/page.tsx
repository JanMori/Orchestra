"use client";

import { IssuesPage } from "@orchestra/views/issues/components";
import { ErrorBoundary } from "@orchestra/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <IssuesPage />
    </ErrorBoundary>
  );
}
