"use client";

import { DashboardLayout } from "@orchestra/views/layout";
import { MulticaIcon } from "@orchestra/ui/components/common/multica-icon";
import { SearchCommand, SearchTrigger } from "@orchestra/views/search";
import { FloatingChat } from "@orchestra/views/chat";
import { WebNotificationBridge } from "@/components/web-notification-bridge";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <DashboardLayout
      loadingIndicator={<MulticaIcon className="size-6" />}
      searchSlot={<SearchTrigger />}
      extra={
        <>
          <SearchCommand />
          <WebNotificationBridge />
          <FloatingChat />
        </>
      }
    >
      {children}
    </DashboardLayout>
  );
}
