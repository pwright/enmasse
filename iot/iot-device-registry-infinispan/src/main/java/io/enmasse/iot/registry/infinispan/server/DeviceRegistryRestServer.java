/*
 * Copyright 2019, EnMasse authors.
 * License: Apache License 2.0 (see the file LICENSE or http://apache.org/licenses/LICENSE-2.0.html).
 */

package io.enmasse.iot.registry.infinispan.server;

import org.eclipse.hono.config.ServiceConfigProperties;
import org.eclipse.hono.service.http.HttpServiceBase;
import org.springframework.stereotype.Component;

@Component
public class DeviceRegistryRestServer extends HttpServiceBase<ServiceConfigProperties> {
}
