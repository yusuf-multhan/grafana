import { useGetSingle } from 'app/features/plugins/admin/state/hooks';
import { CatalogPlugin } from 'app/features/plugins/admin/types';
import { Receiver } from 'app/plugins/datasource/alertmanager/types';

import { useGetOnCallIntegrations } from '../../../api/onCallApi';

import { isOnCallReceiver } from './onCall/onCall';
import { AmRouteReceiver, GrafanaAppReceiverEnum, GRAFANA_APP_PLUGGIN_IDS, ReceiverWithTypes } from './types';

export const useGetAppIsInstalledAndEnabled = (grafanaAppType: GrafanaAppReceiverEnum) => {
  const plugin: CatalogPlugin | undefined = useGetSingle(GRAFANA_APP_PLUGGIN_IDS[grafanaAppType]);
  return plugin?.isInstalled && !plugin?.isDisabled && plugin?.isPublished && plugin?.type === 'app'; // fetches the plugin settings for this Grafana instance
};

export const useGetGrafanaReceiverTypeChecker = () => {
  const isOnCallEnabled = useGetAppIsInstalledAndEnabled(GrafanaAppReceiverEnum.GRAFANA_ONCALL);
  const data = useGetOnCallIntegrations(!isOnCallEnabled);

  const getGrafanaReceiverType = (receiver: Receiver): GrafanaAppReceiverEnum | undefined => {
    //CHECK FOR ONCALL PLUGIN
    const onCallIntegrations = data ?? [];
    if (isOnCallEnabled && isOnCallReceiver(receiver, onCallIntegrations)) {
      return GrafanaAppReceiverEnum.GRAFANA_ONCALL;
    }
    //WE WILL ADD IN HERE IF THERE ARE MORE TYPES TO CHECK
    return;
  };
  return getGrafanaReceiverType;
};

export const useGetAmRouteReceiverWithGrafanaAppTypes = (receivers: Receiver[]) => {
  // FOR GRAFANA ONCALL
  const getGrafanaReceiverType = useGetGrafanaReceiverTypeChecker();
  const receiverToSelectableContactPointValue = (receiver: Receiver): AmRouteReceiver => {
    const amRouteReceiverValue: AmRouteReceiver = {
      label: receiver.name,
      value: receiver.name,
      grafanaAppReceiverType: getGrafanaReceiverType(receiver),
    };
    return amRouteReceiverValue;
  };

  return receivers.map((receiver: Receiver) => receiverToSelectableContactPointValue(receiver));
};

export const useGetReceiversWithGrafanaAppTypes = (receivers: Receiver[]): ReceiverWithTypes[] => {
  // FOR GRAFANA ONCALL
  const getGrafanaReceiverType = useGetGrafanaReceiverTypeChecker();

  return receivers.map((receiver: Receiver) => {
    return {
      ...receiver,
      grafanaAppReceiverType: getGrafanaReceiverType(receiver),
    };
  });
};
