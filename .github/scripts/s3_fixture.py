"""CI-only S3 fixture; requires moto[server]==5.2.5.

Moto checks If-Match before updating the object without a shared operation lock.
Serving one request at a time makes that check and update atomic, as required by
Allot's S3 contract. Clients still race with stale reads and must retry failed CAS.
"""

from moto.server import DomainDispatcherApplication, create_backend_app
from werkzeug.serving import run_simple

if __name__ == "__main__":
    run_simple("127.0.0.1", 5000, DomainDispatcherApplication(create_backend_app), threaded=False)
