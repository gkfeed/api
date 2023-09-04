docker exec -it gkfeed-redis ifconfig eth0 | grep 'inet addr' | sed -e 's/:/ /' | awk '{print $3}'
