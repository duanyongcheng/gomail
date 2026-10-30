import { useMutation, useQueryClient } from "@tanstack/react-query";
import { messageFlagsMutationOptions } from "./messageFlagsMutation";
import { useTranslation } from "react-i18next";

export function useUpdateFlagsMutation() {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  return useMutation(messageFlagsMutationOptions(queryClient, t));
}
