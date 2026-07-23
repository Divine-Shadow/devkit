{
  count = 2;
  candidateParent = "/var/lib/product-adapter-candidates";
  supervisorRoot = "/run/devkit/product-consumers";
  consumers = [
    {
      index = 1;
      name = "product1";
      uid = 2001;
      projection = "a";
    }
    {
      index = 2;
      name = "product2";
      uid = 2002;
      projection = "b";
    }
  ];
}
