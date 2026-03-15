#!/bin/bash

apt install -y postgresql-common
/usr/share/postgresql-common/pgdg/apt.postgresql.org.sh

apt install curl ca-certificates
install -d /usr/share/postgresql-common/pgdg
curl -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc --fail https://www.postgresql.org/media/keys/ACCC4CF8.asc
. /etc/os-release
sh -c "echo 'deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt $VERSION_CODENAME-pgdg main' > /etc/apt/sources.list.d/pgdg.list"
apt update

apt install postgresql-18

# Create the data directory
mkdir -p /var/lib/postgresql/data
chown postgres:postgres /var/lib/postgresql/data

# Switch to the postgres user to initialize
su - postgres -c "/usr/lib/postgresql/18/bin/initdb -D /var/lib/postgresql/data"
# Allow listening on all interfaces
sed -i "s/#listen_addresses = 'localhost'/listen_addresses = '*'/g" /var/lib/postgresql/data/postgresql.conf

# Trust your host's IP (assuming 172.16.0.1)
echo "host all all 172.16.0.0/24 trust" >>/var/lib/postgresql/data/pg_hba.conf
su - postgres -c "/usr/lib/postgresql/18/bin/pg_ctl -D /var/lib/postgresql/data -l /var/lib/postgresql/data/server.log start"
