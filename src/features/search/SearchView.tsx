import { useState, useCallback, useEffect, useRef } from "react";
import { isFeatureAvailable, unavailableReason } from "@/lib/availability";
import { useQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useTranslation } from "react-i18next";
import { Search, SlidersHorizontal, Loader } from "lucide-react";
import type { AdvancedSearchQuery, SearchHit } from "@/lib/api";
import { advancedSearch, searchMessages } from "@/lib/api";
import { useUIStore } from "@/stores/ui.store";
import { useMailStore } from "@/stores/mail.store";
import SearchFilters from "./SearchFilters";
import SearchResultItem from "./SearchResultItem";
import MessageDetail from "@/components/MessageDetail";
import { useCloseMessageDetail } from "@/hooks/useCloseMessageDetail";
import { getAppTheme } from "@/lib/appThemes";

const emptyFilters: AdvancedSearchQuery = {};

function hasActiveFilters(filters: AdvancedSearchQuery): boolean {
  return !!(
    filters.from ||
    filters.to ||
    filters.subject ||
    filters.dateFrom ||
    filters.dateTo ||
    filters.hasAttachment ||
    filters.folderId
  );
}

export default function SearchView() {
  const { t } = useTranslation();
  const appTheme = useUIStore((s) => s.appTheme);
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<AdvancedSearchQuery>(emptyFilters);
  const [showFilters, setShowFilters] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [hasSearched, setHasSearched] = useState(false);
  const storeSearchQuery = useUIStore((s) => s.searchQuery);
  const activeAccountId = useMailStore((s) => s.activeAccountId);

  const trimmed = query.trim();
  const advancedAvailable = isFeatureAvailable("advancedSearch");
  const filtersActive = advancedAvailable && hasActiveFilters(filters);
  const searchEnabled = hasSearched && (trimmed.length > 0 || filtersActive);

  useCloseMessageDetail(selectedId !== null, () => setSelectedId(null));

  const { data: results = [], isLoading: loading, error: queryError, refetch } = useQuery({
    // `activeAccountId` belongs in the key: switching accounts in the sidebar
    // must re-run the search against the newly selected mailbox instead of
    // serving cached hits from the previous one.
    queryKey: ["search", trimmed, filters, activeAccountId],
    queryFn: () => {
      if (filtersActive) {
        return advancedSearch({
          ...filters,
          text: trimmed || undefined,
          accountId: activeAccountId ?? undefined,
        });
      }
      return searchMessages(trimmed, undefined, activeAccountId ?? undefined);
    },
    enabled: searchEnabled,
    networkMode: filtersActive ? "online" : "always", // Supported basic search reads SQLite.
    staleTime: 60_000,
    placeholderData: (prev: SearchHit[] | undefined) => prev,
  });

  const resultsParentRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: results.length,
    getScrollElement: () => resultsParentRef.current,
    estimateSize: () => getAppTheme(appTheme).rowHeight,
    measureElement: (el) => el.getBoundingClientRect().height,
    overscan: 5,
  });

  useEffect(() => { virtualizer.measure(); }, [appTheme, virtualizer]);

  const doSearch = useCallback(() => {
    const t = query.trim();
    if (!t && !(isFeatureAvailable("advancedSearch") && hasActiveFilters(filters))) {
      setHasSearched(false);
      setSelectedId(null);
      return;
    }
    setHasSearched(true);
    setSelectedId(null);
  }, [query, filters]);

  // Pick up context queries from other views and from this view while mounted.
  useEffect(() => {
    if (!storeSearchQuery) return;
    setQuery(storeSearchQuery);
    setSelectedId(null);
    setHasSearched(true);
    useUIStore.getState().setSearchQuery("");
  }, [storeSearchQuery]);

  // Auto-search when filters change
  useEffect(() => {
    if (hasActiveFilters(filters) || query.trim()) {
      doSearch();
    }
  }, [filters]); // eslint-disable-line react-hooks/exhaustive-deps

  // Debounced type-ahead search (300ms after last keystroke)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    const trimmed = query.trim();
    if (!trimmed && !hasActiveFilters(filters)) return;
    debounceRef.current = setTimeout(() => {
      doSearch();
    }, 300);
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current); };
  }, [query]); // eslint-disable-line react-hooks/exhaustive-deps

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (debounceRef.current) clearTimeout(debounceRef.current);
    doSearch();
  }

  function handleClearFilters() {
    setFilters(emptyFilters);
  }

  return (
    <div className={`search-workspace${selectedId ? " search-workspace--detail-open" : ""}`} style={{ display: "flex", flexDirection: "column", height: "100%", overflow: "hidden" }}>
      <form onSubmit={handleSubmit} className="search-toolbar">
        <div className="search-input-shell">
          <Search size={16} aria-hidden="true" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("inbox.searchPlaceholder")}
            aria-label={t("search.title", "Search")}
            autoFocus
            className="search-input"
          />
        </div>
        <button
          type="submit"
          className="search-toolbar-button search-toolbar-button--primary"
        >
          <Search size={14} aria-hidden="true" />
          {t("search.searchButton")}
        </button>
        <button
          type="button"
          disabled={!advancedAvailable}
          onClick={() => { if (advancedAvailable) setShowFilters(!showFilters); }}
          title={advancedAvailable ? t("search.filters") : unavailableReason()}
          aria-label={t("search.filters")}
          className={`search-toolbar-button search-toolbar-icon-button ${
            showFilters || hasActiveFilters(filters) ? "is-active" : ""
          }`}
        >
          <SlidersHorizontal size={16} />
        </button>
      </form>

      {/* Filters panel */}
      {advancedAvailable && showFilters && (
        <SearchFilters
          filters={filters}
          onChange={setFilters}
          onClear={handleClearFilters}
        />
      )}

      {/* Results + Detail split layout */}
      <div className={`secondary-mail-workspace${selectedId ? " secondary-mail-workspace--detail-open" : ""}`}>
        {/* Results list */}
        <div
          ref={resultsParentRef}
          className="scroll-region search-results-scroll secondary-mail-list"
          style={{
            flexShrink: 0,
            overflow: "auto",
          }}
        >
          {loading && (
            <div className="fade-in mail-state-center">
              <Loader size={20} className="spinner" />
              <span className="mail-state-detail">{t("common.loading")}</span>
            </div>
          )}

          {!loading && queryError && (
            <div className="fade-in mail-state-center">
              <p className="mail-state-error">
                {t("search.error", "Search failed")}
              </p>
              <p className="mail-state-detail">{queryError?.message}</p>
              <button
                type="button"
                className="mail-state-secondary-btn"
                onClick={() => refetch()}
              >
                {t("common.retry", "Retry")}
              </button>
            </div>
          )}

          {!loading && !queryError && hasSearched && results.length === 0 && (
            <div className="mail-state-center">
              <Search size={28} strokeWidth={1.3} />
              <p className="mail-state-detail">{t("search.noResults")}</p>
            </div>
          )}

          {!loading && !hasSearched && (
            <div className="mail-state-center">
              <Search size={30} strokeWidth={1.3} />
              <span className="mail-state-detail">{t("search.title")}</span>
            </div>
          )}

          {!loading && results.length > 0 && (
            <div
              role="listbox"
              aria-label={t("search.results", "Search results")}
              style={{ height: `${virtualizer.getTotalSize()}px`, width: "100%", position: "relative" }}
            >
              {virtualizer.getVirtualItems().map((virtualItem) => {
                const hit = results[virtualItem.index];
                return (
                  <div
                    key={hit.message_id}
                    ref={virtualizer.measureElement}
                    data-index={virtualItem.index}
                    style={{
                      position: "absolute",
                      top: 0,
                      left: 0,
                      width: "100%",
                      transform: `translateY(${virtualItem.start}px)`,
                    }}
                  >
                    <SearchResultItem
                      hit={hit}
                      isSelected={hit.message_id === selectedId}
                      onClick={() => setSelectedId(hit.message_id)}
                      query={query}
                    />
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {/* Detail panel */}
        {selectedId && (
          <div className="secondary-mail-detail">
            <MessageDetail
              messageId={selectedId}
              onBack={() => setSelectedId(null)}
            />
          </div>
        )}
      </div>
    </div>
  );
}
